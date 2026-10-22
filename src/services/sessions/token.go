package sessions

import (
	"context"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

type TokenRequest struct {
	runtime.Connection
	TTLSeconds *int64 `json:"ttlSeconds,omitempty"`
}

// TokenResponse is the only response that exposes the newly issued credential.
type TokenResponse struct {
	ConnectionID string    `json:"connectionID"`
	Revision     int64     `json:"revision"`
	ID           string    `json:"id"`
	Token        string    `json:"token"`
	TokenType    string    `json:"tokenType"`
	ExpiresAt    time.Time `json:"expiresAt"`
	ExpiresIn    int64     `json:"expiresIn"`
}

func IssueToken(ctx context.Context, cfg config.Config, registry ConnectionRegistry, request TokenRequest) (TokenResponse, error) {
	ttl := cfg.WithDefaults().TokenTTL
	if request.TTLSeconds != nil {
		seconds := *request.TTLSeconds
		// Compare before converting to Duration, which can overflow on input.
		if seconds <= 0 || seconds > int64(ttl/time.Second) {
			return TokenResponse{}, apperror.New(400, "InvalidTokenTTL", messages.SessionsInvalidTokenTTL)
		}
		ttl = time.Duration(seconds) * time.Second
	}
	cfg.SessionTTL = ttl
	session, _, token, err := connect(ctx, cfg, registry, request.Connection, runtime.NewBearerSession, true)
	if err != nil {
		return TokenResponse{}, err
	}
	return TokenResponse{ConnectionID: session.ConnectionID(), Revision: 1, ID: session.SessionID(), Token: token, TokenType: "Bearer", ExpiresAt: session.ExpiresAt(), ExpiresIn: int64(ttl / time.Second)}, nil
}

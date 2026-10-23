package sessions

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
)

func TestTokenTTLRejectedBeforeConnecting(t *testing.T) {
	cfg := config.Default()
	cfg.TokenTTL = time.Minute
	for _, seconds := range []int64{-1, 61, math.MaxInt64} {
		_, err := IssueToken(context.Background(), cfg, nil, TokenRequest{TTLSeconds: &seconds})
		if err == nil || apperror.Public(err).Code != "InvalidTokenTTL" {
			t.Fatal("invalid lifetime accepted", seconds, err)
		}
	}
}

package sessions

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	settings "github.com/owiks/golang-minio-s3-backend-api/src/config"
	accesssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/access"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	endpoints "github.com/owiks/golang-minio-s3-backend-api/src/services/endpoint"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	objectssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
)

type LoginResponse struct {
	ConnectionID   string                     `json:"connectionID"`
	ID             string                     `json:"id"`
	CSRF           string                     `json:"csrf"`
	Endpoint       string                     `json:"endpoint"`
	Region         string                     `json:"region"`
	ExpiresAt      time.Time                  `json:"expiresAt"`
	Buckets        []objectssvc.BucketRow     `json:"buckets"`
	Access         accesssvc.NavigationAccess `json:"access"`
	AccessRevision string                     `json:"accessRevision"`
}

func Connect(ctx context.Context, cfg settings.Config, registry ConnectionRegistry, c runtimesvc.Connection) (*runtimesvc.Session, LoginResponse, error) {
	session, response, _, err := connect(ctx, cfg, registry, c, newCookieSession, true)
	return session, response, err
}

type sessionFactory func(runtimesvc.Connection, settings.Config, *url.URL, *url.URL, *http.Transport) (*runtimesvc.Session, string, error)

func newCookieSession(c runtimesvc.Connection, cfg settings.Config, public, internal *url.URL, tr *http.Transport) (*runtimesvc.Session, string, error) {
	s, err := runtimesvc.NewConnectedSession(c, cfg, public, internal, tr)
	return s, "", err
}

func connect(ctx context.Context, cfg settings.Config, registry ConnectionRegistry, c runtimesvc.Connection, factory sessionFactory, publish bool) (*runtimesvc.Session, LoginResponse, string, error) {
	c.AccessKey = strings.TrimSpace(c.AccessKey)
	c.Region = strings.TrimSpace(c.Region)
	c.SessionToken = strings.TrimSpace(c.SessionToken)
	if c.AccessKey == "" || c.SecretKey == "" || len(c.AccessKey) > 256 || len(c.SecretKey) > 4096 || len(c.SessionToken) > 16384 || len(c.Region) > 128 {
		return nil, LoginResponse{}, "", apperror.New(400, "InvalidCredentials", messages.SessionsInvalidCredentials)
	}
	if c.Region == "" {
		c.Region = "us-east-1"
	}
	endpoint, err := endpoints.Parse(c.Endpoint)
	if err != nil {
		return nil, LoginResponse{}, "", err
	}
	if err := registry.AdmitIdentity(endpoint.String(), c.AccessKey); err != nil {
		return nil, LoginResponse{}, "", err
	}
	tr, target, err := endpoints.Transport(ctx, cfg.Endpoints, endpoint)
	if err != nil {
		return nil, LoginResponse{}, "", err
	}
	accepted := false
	defer func() {
		if !accepted {
			tr.CloseIdleConnections()
		}
	}()
	s, token, err := factory(c, cfg, endpoint, target, tr)
	if err != nil {
		return nil, LoginResponse{}, "", err
	}
	accessRevision := registry.AccessRevision(s)
	buckets, access, err := accesssvc.DiscoverAccess(ctx, runtimesvc.NewClients(s))
	if err != nil {
		return nil, LoginResponse{}, "", err
	}
	if publish {
		if err := registry.StoreSessionContext(ctx, s); err != nil {
			return nil, LoginResponse{}, "", err
		}
		accepted = true
	}
	telemetry.Identify(ctx, s.ActorName(), s.InternalHost())
	if publish {
		telemetry.Event(ctx, "session created")
	}
	return s, LoginResponse{ConnectionID: s.ConnectionID(), ID: s.SessionID(), CSRF: s.CSRFToken(), Endpoint: endpoint.String(), Region: c.Region, ExpiresAt: s.ExpiresAt(), Buckets: buckets, Access: access, AccessRevision: accessRevision}, token, nil
}

// VerifyCredentials performs the same endpoint, rate and upstream checks as
// login without publishing a connection or credential.
func VerifyCredentials(ctx context.Context, cfg settings.Config, registry ConnectionRegistry, c runtimesvc.Connection) error {
	_, _, _, err := connect(ctx, cfg, registry, c, newCookieSession, false)
	return err
}

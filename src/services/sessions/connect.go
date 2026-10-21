package sessions

import (
	"context"
	"strings"
	"time"

	settings "minio-web/api/src/config"
	accesssvc "minio-web/api/src/services/access"
	"minio-web/api/src/services/apperror"
	endpoints "minio-web/api/src/services/endpoint"
	objectssvc "minio-web/api/src/services/objects"
	runtimesvc "minio-web/api/src/services/runtime"
	"minio-web/api/src/services/telemetry"
)

type LoginResponse struct {
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
	c.AccessKey = strings.TrimSpace(c.AccessKey)
	c.Region = strings.TrimSpace(c.Region)
	c.SessionToken = strings.TrimSpace(c.SessionToken)
	if c.AccessKey == "" || c.SecretKey == "" || len(c.AccessKey) > 256 || len(c.SecretKey) > 4096 || len(c.SessionToken) > 16384 || len(c.Region) > 128 {
		return nil, LoginResponse{}, apperror.New(400, "InvalidCredentials", "Введите корректные Access Key, Secret Key и регион.")
	}
	if c.Region == "" {
		c.Region = "us-east-1"
	}
	endpoint, err := endpoints.Parse(c.Endpoint)
	if err != nil {
		return nil, LoginResponse{}, err
	}
	if err := registry.AdmitIdentity(endpoint.String(), c.AccessKey); err != nil {
		return nil, LoginResponse{}, err
	}
	tr, target, err := endpoints.Transport(ctx, cfg.Endpoints, endpoint)
	if err != nil {
		return nil, LoginResponse{}, err
	}
	accepted := false
	defer func() {
		if !accepted {
			tr.CloseIdleConnections()
		}
	}()
	s, err := runtimesvc.NewConnectedSession(c, cfg, endpoint, target, tr)
	if err != nil {
		return nil, LoginResponse{}, err
	}
	accessRevision := registry.AccessRevision(s)
	buckets, access, err := accesssvc.DiscoverAccess(ctx, runtimesvc.NewClients(s))
	if err != nil {
		return nil, LoginResponse{}, err
	}
	if err := registry.StoreSession(s); err != nil {
		return nil, LoginResponse{}, err
	}
	accepted = true
	telemetry.Identify(ctx, s.ActorName(), s.InternalHost())
	telemetry.Event(ctx, "session created")
	return s, LoginResponse{ID: s.SessionID(), CSRF: s.CSRFToken(), Endpoint: endpoint.String(), Region: c.Region, ExpiresAt: s.ExpiresAt(), Buckets: buckets, Access: access, AccessRevision: accessRevision}, nil
}

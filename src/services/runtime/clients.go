package runtime

import (
	"context"
	"net/http"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
)

func (s *Session) clientConfig() storage.Config {
	return storage.Config{Endpoint: s.internal, Credentials: s.credentials, Region: s.region}
}

func (s *Session) requestS3(ctx context.Context) (*minio.Client, error) {
	return storage.NewS3(s.clientConfig(), storage.WithContext(ctx, telemetry.Transport{Base: s.requestTransport()}))
}

func (s *Session) newAdmin(transport http.RoundTripper) (*storage.AdminClient, error) {
	return storage.NewAdmin(s.clientConfig(), storage.ScopeTransport{Base: transport, Policy: s.AccessPolicy(), Load: s.livePolicy, Admin: true})
}

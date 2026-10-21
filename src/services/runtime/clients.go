package runtime

import (
	"context"
	"net/http"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/storage"
	"minio-web/api/src/services/telemetry"
)

func (s *Session) clientConfig() storage.Config {
	return storage.Config{Endpoint: s.internal, Credentials: s.credentials, Region: s.region}
}

func (s *Session) newS3(transport http.RoundTripper) (*minio.Client, error) {
	return storage.NewS3(s.clientConfig(), transport)
}

func (s *Session) requestS3(ctx context.Context) (*minio.Client, error) {
	return s.newS3(storage.WithContext(ctx, telemetry.Transport{Base: s.transport}))
}

func (s *Session) newAdmin(transport http.RoundTripper) (*storage.AdminClient, error) {
	return storage.NewAdmin(s.clientConfig(), transport)
}

package sessions

import (
	"context"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

// CredentialVerifier adapts native login validation without publishing another
// connection. It satisfies the consumer-owned authstate verifier port.
type CredentialVerifier struct {
	config   config.Config
	registry ConnectionRegistry
}

func NewCredentialVerifier(cfg config.Config, registry ConnectionRegistry) CredentialVerifier {
	return CredentialVerifier{config: cfg.Clone(), registry: registry}
}
func (v CredentialVerifier) Verify(ctx context.Context, c authstate.ConnectionView, secrets authstate.Secrets) error {
	return VerifyCredentials(ctx, v.config, v.registry, runtime.Connection{Endpoint: c.Endpoint, Region: c.Region, AccessKey: secrets.AccessKey, SecretKey: secrets.SecretKey, SessionToken: secrets.SessionToken})
}

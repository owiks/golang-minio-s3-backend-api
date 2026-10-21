package runtime

import (
	"context"
	"net/url"
	"time"

	"minio-web/api/src/services/capabilities"
	"minio-web/api/src/services/storage"
	"minio-web/api/src/services/uploads/multipart"
)

// MultipartClients binds internal S3 operations and public signatures to the
// same authenticated session. Domain code receives only its consumer-owned port.
func (app *State) MultipartClients(s *Session) multipart.Client {
	return &multipartClients{Clients: NewClients(s), signer: storage.NewS3Client(s.signer), observer: app.CapabilityObserver(s)}
}

type multipartClients struct {
	*Clients
	signer   *storage.S3Client
	observer capabilities.Observer
}

func (c *multipartClients) SessionExpiresAt() time.Time { return c.session.expires }
func (c *multipartClients) ConditionalCompletionConfigured() bool {
	return c.session.conditionalComplete
}
func (c *multipartClients) PresignMultipartPart(ctx context.Context, bucket, key, uploadID string, part int, checksum string, expiry time.Duration) (*url.URL, error) {
	return c.signer.PresignMultipartPart(ctx, bucket, key, uploadID, part, checksum, expiry)
}

func (c *multipartClients) ObserveMultipart(ctx context.Context, operation, bucket, key string, err error) {
	var feature capabilities.Identifier
	switch operation {
	case "create":
		feature = capabilities.MultipartCreate
	case "part":
		feature = capabilities.MultipartPartPut
	case "list":
		feature = capabilities.MultipartPartsList
	case "complete":
		feature = capabilities.MultipartComplete
	case "abort":
		feature = capabilities.MultipartAbort
	default:
		return
	}
	// Success proves this operation, never that native conditions were enforced.
	c.observer.Observe(ctx, capabilities.Observation{ID: feature, Scope: capabilities.Scope{Bucket: bucket, Key: key}, Err: err})
}

package runtime

import (
	"context"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

func (c *Clients) ReadS3Metrics(ctx context.Context) ([]byte, error) {
	return storage.ReadS3Metrics(ctx, c.session.clientConfig(), c.session.adminTransport())
}
func (c *Clients) ProbeDiagnostic(ctx context.Context, trace bool) error {
	return storage.ProbeDiagnostic(ctx, c.session.clientConfig(), c.session.adminTransport(), trace)
}

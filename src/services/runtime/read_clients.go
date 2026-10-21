package runtime

import (
	"context"

	"minio-web/api/src/services/storage"
)

func (c *Clients) ReadS3Metrics(ctx context.Context) ([]byte, error) {
	return storage.ReadS3Metrics(ctx, c.session.clientConfig(), c.session.transport)
}
func (c *Clients) ProbeDiagnostic(ctx context.Context, trace bool) error {
	return storage.ProbeDiagnostic(ctx, c.session.clientConfig(), c.session.transport, trace)
}

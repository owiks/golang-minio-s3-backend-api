package runtime

import (
	"context"

	"github.com/minio/minio-go/v7"
	"minio-web/api/src/services/storage"
)

func (c *Clients) OpenSelect(ctx context.Context, bucket, key string, options minio.SelectObjectOptions) (storage.SelectStream, error) {
	return storage.OpenSelect(ctx, c.session.clientConfig(), c.session.transport, bucket, key, options)
}

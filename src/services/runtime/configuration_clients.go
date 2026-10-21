package runtime

import (
	"context"

	"github.com/minio/minio-go/v7/pkg/cors"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
	"github.com/minio/minio-go/v7/pkg/sse"

	"minio-web/api/src/services/storage"
)

func (c *Clients) ReadLifecycleConfig(ctx context.Context, bucket string) (storage.ConfigRead[*lifecycle.Configuration], error) {
	return storage.ReadLifecycleConfig(ctx, c.session.clientConfig(), c.session.transport, bucket)
}
func (c *Clients) WriteLifecycleConfig(ctx context.Context, bucket string, value *lifecycle.Configuration) (int, error) {
	return storage.WriteLifecycleConfig(ctx, c.session.clientConfig(), c.session.transport, bucket, value)
}
func (c *Clients) ReadCORSConfig(ctx context.Context, bucket string) (storage.ConfigRead[*cors.Config], error) {
	return storage.ReadCORSConfig(ctx, c.session.clientConfig(), c.session.transport, bucket)
}
func (c *Clients) WriteCORSConfig(ctx context.Context, bucket string, value *cors.Config) (int, error) {
	return storage.WriteCORSConfig(ctx, c.session.clientConfig(), c.session.transport, bucket, value)
}
func (c *Clients) ReadEncryptionConfig(ctx context.Context, bucket string) (storage.ConfigRead[*sse.Configuration], error) {
	return storage.ReadEncryptionConfig(ctx, c.session.clientConfig(), c.session.transport, bucket)
}
func (c *Clients) WriteEncryptionConfig(ctx context.Context, bucket string, value *sse.Configuration) (int, error) {
	return storage.WriteEncryptionConfig(ctx, c.session.clientConfig(), c.session.transport, bucket, value)
}

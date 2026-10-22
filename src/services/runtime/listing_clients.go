package runtime

import (
	"context"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

// Core's listing API has no context parameter. A request-scoped client binds
// the transport to this operation while retaining the session connection pool,
// native continuation tokens and the existing single-attempt retry policy.
func (c *Clients) ListObjectPage(ctx context.Context, bucket, prefix, token, delimiter string, maxKeys int) (minio.ListBucketV2Result, error) {
	client, err := c.session.requestS3(ctx)
	if err != nil {
		return minio.ListBucketV2Result{}, err
	}
	return (minio.Core{Client: client}).ListObjectsV2(bucket, prefix, "", token, delimiter, maxKeys)
}

func (c *Clients) ReadVersionPage(ctx context.Context, bucket, prefix, keyMarker, versionMarker string, maxKeys int) (storage.VersionPage, error) {
	return storage.ReadVersionPage(ctx, c.session.s3, c.session.requestTransport(), bucket, prefix, keyMarker, versionMarker, maxKeys)
}

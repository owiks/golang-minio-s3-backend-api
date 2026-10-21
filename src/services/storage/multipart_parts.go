package storage

import (
	"context"
	"io"

	"github.com/minio/minio-go/v7"
)

func (c *S3Client) PutObjectPart(ctx context.Context, bucket, key, uploadID string, part int, body io.Reader, size int64, opts minio.PutObjectPartOptions) (minio.ObjectPart, error) {
	return (minio.Core{Client: c.Client}).PutObjectPart(ctx, bucket, key, uploadID, part, body, size, opts)
}

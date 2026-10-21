package selectquery

import (
	"context"

	"github.com/minio/minio-go/v7"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

type Client interface {
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
	OpenSelect(context.Context, string, string, minio.SelectObjectOptions) (storage.SelectStream, error)
}

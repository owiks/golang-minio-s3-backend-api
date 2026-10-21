package selectquery

import (
	"context"

	"github.com/minio/minio-go/v7"
	"minio-web/api/src/services/storage"
)

type Client interface {
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
	OpenSelect(context.Context, string, string, minio.SelectObjectOptions) (storage.SelectStream, error)
}

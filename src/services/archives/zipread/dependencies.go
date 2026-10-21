package zipread

import (
	"context"

	"github.com/minio/minio-go/v7"
)

type ObjectReadClient interface {
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
}
type ZIPSource interface {
	ObjectReadClient
	SessionID() string
}

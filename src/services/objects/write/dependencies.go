package objectwrite

import (
	"context"

	"github.com/minio/minio-go/v7"
)

// StateReader exposes only the destination lookup needed before writing.
type StateReader interface {
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

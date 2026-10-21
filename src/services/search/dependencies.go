package search

import (
	"context"

	"github.com/minio/minio-go/v7"
)

// Interfaces belong to the use cases that consume them. Only required
// operations are exposed; connection credentials and session state stay outside.
type ReadSearchTextClient interface {
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
}

type RecursivePageClient interface {
	ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error)
}

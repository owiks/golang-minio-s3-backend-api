package foldershare

import (
	"context"

	"github.com/minio/minio-go/v7"
)

// ReadClient is the delegated read capability retained until this link expires.
type ReadClient interface {
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
	ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error)
}

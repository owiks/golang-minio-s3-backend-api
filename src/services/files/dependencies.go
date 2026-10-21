package files

import (
	"context"
	"net/url"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

// Interfaces belong to the use cases that consume them. Only required
// operations are exposed; connection credentials and session state stay outside.
type GetObjectInfoClient interface {
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type GetObjectLinkClient interface {
	PresignedGetObject(context.Context, string, string, time.Duration, url.Values) (*url.URL, error)
}

type GetPreviewClient interface {
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type GetVersionProtectionClient interface {
	GetObjectLegalHold(context.Context, string, string, minio.GetObjectLegalHoldOptions) (*minio.LegalHoldStatus, error)
	GetObjectRetention(context.Context, string, string, string) (*minio.RetentionMode, *time.Time, error)
}

type GetVersionsClient interface {
	ReadVersionPage(context.Context, string, string, string, string, int) (storage.VersionPage, error)
}

type ListBucketsClient interface {
	AccountInfo(context.Context, madmin.AccountOpts) (madmin.AccountInfo, error)
	ListBuckets(context.Context) ([]minio.BucketInfo, error)
	ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error)
}

type ListObjectsClient interface {
	ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error)
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type ReadObjectUsageClient interface {
	ReadVersionPage(context.Context, string, string, string, string, int) (storage.VersionPage, error)
	ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error)
}

type SearchContentClient interface {
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
	ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error)
}

type UpdateVersionProtectionClient interface {
	GetObjectLegalHold(context.Context, string, string, minio.GetObjectLegalHoldOptions) (*minio.LegalHoldStatus, error)
	GetObjectRetention(context.Context, string, string, string) (*minio.RetentionMode, *time.Time, error)
	PutObjectLegalHold(context.Context, string, string, minio.PutObjectLegalHoldOptions) error
	PutObjectRetention(context.Context, string, string, minio.PutObjectRetentionOptions) error
}

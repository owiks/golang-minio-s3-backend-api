package transfers

import (
	"context"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/tags"
)

// Interfaces belong to the use cases that consume them. Only required
// operations are exposed; connection credentials and session state stay outside.
type PrepareFolderClient interface {
	ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error)
}

type TransferClient interface {
	AuthorizeResource(context.Context, string, string, string) error
	AbortMultipartUpload(context.Context, string, string, string) error
	CompleteMultipartUpload(context.Context, string, string, string, []minio.CompletePart, minio.PutObjectOptions) (minio.UploadInfo, error)
	ConditionalDeleteSupported() bool
	CopyObjectPart(context.Context, string, string, string, string, string, int, int64, int64, map[string]string) (minio.CompletePart, error)
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
	GetObjectTagging(context.Context, string, string, minio.GetObjectTaggingOptions) (*tags.Tags, error)
	ListObjects(context.Context, string, minio.ListObjectsOptions) <-chan minio.ObjectInfo
	NewMultipartUpload(context.Context, string, string, minio.PutObjectOptions) (string, error)
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
	RemoveConditional(context.Context, string, string, string) error
	RemoveObject(context.Context, string, string, minio.RemoveObjectOptions) error
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type CopyOnServerClient interface {
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
	AbortMultipartUpload(context.Context, string, string, string) error
	CompleteMultipartUpload(context.Context, string, string, string, []minio.CompletePart, minio.PutObjectOptions) (minio.UploadInfo, error)
	CopyObjectPart(context.Context, string, string, string, string, string, int, int64, int64, map[string]string) (minio.CompletePart, error)
	NewMultipartUpload(context.Context, string, string, minio.PutObjectOptions) (string, error)
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
}

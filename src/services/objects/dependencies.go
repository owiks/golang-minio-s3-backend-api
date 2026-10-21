package objects

import (
	"context"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/tags"

	"minio-web/api/src/services/storage"
)

// Interfaces belong to the use cases that consume them. Only required
// operations are exposed; connection credentials and session state stay outside.
type StreamClient interface {
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type DeleteObjectClient interface {
	RemoveObject(context.Context, string, string, minio.RemoveObjectOptions) error
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type GetContentClient interface {
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
	GetObjectTagging(context.Context, string, string, minio.GetObjectTaggingOptions) (*tags.Tags, error)
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type GetPropertiesClient interface {
	GetObjectTagging(context.Context, string, string, minio.GetObjectTaggingOptions) (*tags.Tags, error)
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type ListVersionsClient interface {
	ReadVersionPage(context.Context, string, string, string, string, int) (storage.VersionPage, error)
}

type MultipartRewriteClient interface {
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
	AbortMultipartUpload(context.Context, string, string, string) error
	CompleteMultipartUpload(context.Context, string, string, string, []minio.CompletePart, minio.PutObjectOptions) (minio.UploadInfo, error)
	CopyObjectPart(context.Context, string, string, string, string, string, int, int64, int64, map[string]string) (minio.CompletePart, error)
	NewMultipartUpload(context.Context, string, string, minio.PutObjectOptions) (string, error)
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
}

type PrepareChecksumClient interface {
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type ReadObjectProtectionClient interface {
	GetObjectLegalHold(context.Context, string, string, minio.GetObjectLegalHoldOptions) (*minio.LegalHoldStatus, error)
	GetObjectRetention(context.Context, string, string, string) (*minio.RetentionMode, *time.Time, error)
}

type RewriteObjectClient interface {
	AbortMultipartUpload(context.Context, string, string, string) error
	CompleteMultipartUpload(context.Context, string, string, string, []minio.CompletePart, minio.PutObjectOptions) (minio.UploadInfo, error)
	CopyObjectPart(context.Context, string, string, string, string, string, int, int64, int64, map[string]string) (minio.CompletePart, error)
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
	NewMultipartUpload(context.Context, string, string, minio.PutObjectOptions) (string, error)
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
}

type UpdateContentClient interface {
	GetObjectTagging(context.Context, string, string, minio.GetObjectTaggingOptions) (*tags.Tags, error)
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type UpdatePropertiesClient interface {
	AbortMultipartUpload(context.Context, string, string, string) error
	CompleteMultipartUpload(context.Context, string, string, string, []minio.CompletePart, minio.PutObjectOptions) (minio.UploadInfo, error)
	CopyObjectPart(context.Context, string, string, string, string, string, int, int64, int64, map[string]string) (minio.CompletePart, error)
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
	GetObjectTagging(context.Context, string, string, minio.GetObjectTaggingOptions) (*tags.Tags, error)
	NewMultipartUpload(context.Context, string, string, minio.PutObjectOptions) (string, error)
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
	PutObjectTagging(context.Context, string, string, *tags.Tags, minio.PutObjectTaggingOptions) error
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

// ReadAttributesClient exposes validated, bounded attributes without the SDK client.
type ReadAttributesClient interface {
	ReadObjectAttributes(context.Context, string, string, storage.ObjectAttributesOptions) (storage.ObjectAttributesPage, error)
}

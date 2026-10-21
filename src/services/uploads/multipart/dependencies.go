// Package multipart owns managed, session-scoped resumable uploads.
package multipart

import (
	"context"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/iam"
)

type Client interface {
	SessionID() string
	SessionExpiresAt() time.Time
	ActorName() string
	ActionPolicySnapshot() *iam.Policy
	ConditionalCompletionConfigured() bool
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
	NewMultipartUpload(context.Context, string, string, minio.PutObjectOptions) (string, error)
	PutObjectPart(context.Context, string, string, string, int, io.Reader, int64, minio.PutObjectPartOptions) (minio.ObjectPart, error)
	ListObjectParts(context.Context, string, string, string, int, int) (minio.ListObjectPartsResult, error)
	CompleteMultipartUpload(context.Context, string, string, string, []minio.CompletePart, minio.PutObjectOptions) (minio.UploadInfo, error)
	AbortMultipartUpload(context.Context, string, string, string) error
	PresignMultipartPart(context.Context, string, string, string, int, string, time.Duration) (*url.URL, error)
}

// Store only publishes opaque operations. The implementation owns admission and
// expiry; neither S3 upload IDs nor mutable operation fields escape the model.
type Store interface {
	Add(*Operation) error
	Get(string) *Operation
	Discard(*Operation)
}

type observer interface {
	ObserveMultipart(context.Context, string, string, string, error)
}

func observe(ctx context.Context, c Client, name, bucket, key string, err error) {
	if v, ok := c.(observer); ok {
		v.ObserveMultipart(ctx, name, bucket, key, err)
	}
}

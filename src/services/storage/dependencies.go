package storage

import (
	"context"
	"io"

	"github.com/minio/minio-go/v7"
)

// Interfaces belong to the use cases that consume them. Only required
// operations are exposed; connection credentials and session state stay outside.
type CopyClient interface {
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
	AbortMultipartUpload(context.Context, string, string, string) error
	CompleteMultipartUpload(context.Context, string, string, string, []minio.CompletePart, minio.PutObjectOptions) (minio.UploadInfo, error)
	CopyObjectPart(context.Context, string, string, string, string, string, int, int64, int64, map[string]string) (minio.CompletePart, error)
	NewMultipartUpload(context.Context, string, string, minio.PutObjectOptions) (string, error)
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
}

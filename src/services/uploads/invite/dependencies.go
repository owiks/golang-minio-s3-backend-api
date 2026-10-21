package uploadinvite

import (
	"context"
	"io"

	"github.com/minio/minio-go/v7"
)

type WriteClient interface {
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
}

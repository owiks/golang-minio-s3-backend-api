package transfers

import (
	"context"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

func copyOnServer(ctx context.Context, s CopyOnServerClient, body FileTransferRequest, source minio.ObjectInfo, opts minio.PutObjectOptions) (minio.UploadInfo, bool, error) {
	return storage.CopyConditional(ctx, s, storage.CopyRequest{SourceBucket: body.SourceBucket, SourceKey: body.SourceKey, DestinationBucket: body.DestinationBucket, DestinationKey: body.DestinationKey}, source, opts, 64*1024*1024)
}

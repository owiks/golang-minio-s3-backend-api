package objects

import (
	"context"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

func RewriteObject(ctx context.Context, s RewriteObjectClient, bucket, key string, info minio.ObjectInfo, opts minio.PutObjectOptions) error {
	_, handled, err := storage.CopyConditional(ctx, s, storage.CopyRequest{SourceBucket: bucket, SourceKey: key, DestinationBucket: bucket, DestinationKey: key}, info, opts, 64*1024*1024)
	if handled {
		return err
	}
	// Only an explicit unsupported part-copy may use conditional streaming PUT.
	get := minio.GetObjectOptions{VersionID: info.VersionID}
	_ = get.SetMatchETag(info.ETag)
	object, err := s.GetObject(ctx, bucket, key, get)
	if err != nil {
		return err
	}
	defer object.Close()
	_, err = s.PutObject(ctx, bucket, key, object, info.Size, opts)
	return err
}
func MultipartRewrite(ctx context.Context, client MultipartRewriteClient, bucket, key string, info minio.ObjectInfo, opts minio.PutObjectOptions, partSize int64) error {
	opts.SetMatchETag(info.ETag)
	_, handled, err := storage.CopyConditional(ctx, client, storage.CopyRequest{SourceBucket: bucket, SourceKey: key, DestinationBucket: bucket, DestinationKey: key}, info, opts, partSize)
	if !handled {
		return apperror.New(501, "CopyUnsupported", messages.ObjectsCopyUnsupported)
	}
	return err
}

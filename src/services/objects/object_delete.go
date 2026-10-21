package objects

import (
	"context"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/objects/state"
)

func ReadDeletion(ctx context.Context, s StreamClient, bucket, key string) (objectstate.DeletionSnapshot, error) {
	info, err := s.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	return objectstate.DeletionSnapshot{ETag: info.ETag, Size: info.Size, Modified: info.LastModified, Version: info.VersionID}, err
}

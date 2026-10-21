package buckets

import (
	"context"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/s3utils"

	"minio-web/api/src/services/apperror"
)

type CreateBucketRequest struct {
	Name       string `json:"name" validate:"required"`
	ObjectLock bool   `json:"objectLock,omitempty"`
}

func CreateBucket(ctx context.Context, lock func(context.Context, string) (func(), error), s CreateBucketClient, b CreateBucketRequest) error {
	if s3utils.CheckValidBucketNameStrict(b.Name) != nil {
		return apperror.New(400, "InvalidBucketName", "Имя бакета: 3–63 строчных латинских символа, цифры, точки и дефисы; имя не должно быть IP-адресом.")
	}
	release, lockErr := lock(ctx, "bucket/"+b.Name)
	if lockErr != nil {
		return lockErr
	}
	defer release()
	// S3 CreateBucket can succeed for an already-owned bucket on some servers.
	exists, err := s.BucketExists(ctx, b.Name)
	if err != nil {
		return err
	}
	if exists {
		return apperror.New(409, "AlreadyExists", "Бакет с таким именем уже существует.")
	}
	if err := s.MakeBucket(ctx, b.Name, minio.MakeBucketOptions{Region: s.RegionName(), ObjectLocking: b.ObjectLock}); err != nil {
		return err
	}
	return nil
}
func DeleteBucket(ctx context.Context, s DeleteBucketClient, name string) error {
	return s.RemoveBucket(ctx, name)
}

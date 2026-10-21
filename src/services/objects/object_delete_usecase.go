package objects

import (
	"context"
	"strings"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	objectstate "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/state"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/result"
)

func InspectDeletion(ctx context.Context, s StreamClient, bucket, key string) (DeletionInfo, error) {
	bucket, key, err := ObjectParams(bucket, key)
	if err != nil {
		return DeletionInfo{}, err
	}
	if strings.HasSuffix(key, "/") {
		return DeletionInfo{}, apperror.New(400, "InvalidDelete", messages.HttpInvalidDelete)
	}

	current, err := ReadDeletion(ctx, s, bucket, key)
	if err != nil {
		return DeletionInfo{}, err
	}
	return DeletionInfo{Revision: objectstate.DeletionRevision(current), Size: current.Size, Modified: current.Modified}, nil
}

func DeleteObject(ctx context.Context, s DeleteObjectClient, bucket, key string, body ObjectDeleteRequest) (result.Success, error) {
	bucket, key, err := ObjectParams(bucket, key)
	if err != nil {
		return result.Success{}, err
	}
	if strings.HasSuffix(key, "/") {
		return result.Success{}, apperror.New(400, "InvalidDelete", messages.HttpInvalidDelete)
	}

	if body.Confirmation != key {
		return result.Success{}, apperror.New(400, "ConfirmationRequired", messages.ObjectsConfirmationRequired)
	}
	if body.Revision == "" {
		return result.Success{}, apperror.New(400, "RevisionRequired", messages.ObjectsRevisionRequired)
	}

	current, err := ReadDeletion(ctx, s, bucket, key)
	if err != nil {
		return result.Success{}, err
	}

	if body.Revision != objectstate.DeletionRevision(current) {
		return result.Success{}, apperror.New(409, "ObjectChanged", messages.ObjectsObjectChangedObjectDeleteUsecase)
	}
	// Ordinary S3 deletion only: never target a version, force deletion, recursively
	// remove a prefix, or bypass GOVERNANCE. MinIO enforces the caller's rights.
	err = s.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		switch minio.ToErrorResponse(err).Code {
		case "AccessDenied", "InvalidRequest", "ObjectLocked":
			return result.Success{}, apperror.New(403, "ObjectDeleteDenied", messages.ObjectsObjectDeleteDenied)
		}
		return result.Success{}, err
	}
	return result.Success{OK: true}, nil
}

type ObjectDeleteRequest struct {
	Revision     string `json:"revision"`
	Confirmation string `json:"confirmation"`
}

// Package objectwrite owns destination snapshots and conditional write rules
// shared by uploads, transfers, extraction and object updates.
package objectwrite

import (
	"context"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	objectstate "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/state"
)

const MaxFileSize int64 = 5 * 1024 * 1024 * 1024

type FileState struct {
	Exists   bool   `json:"exists"`
	Revision string `json:"revision"`
	ETag     string `json:"etag,omitempty"`
	Size     int64  `json:"size"`
}

func DestinationState(ctx context.Context, s StateReader, bucket, key string) (FileState, error) {
	info, err := s.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if minio.ToErrorResponse(err).Code == "NoSuchKey" {
		return FileState{Revision: "absent"}, nil
	}
	if err != nil {
		return FileState{}, err
	}
	snapshot := objectstate.DeletionSnapshot{ETag: info.ETag, Size: info.Size, Modified: info.LastModified, Version: info.VersionID}
	return FileState{Exists: true, Revision: objectstate.DeletionRevision(snapshot), ETag: info.ETag, Size: info.Size}, nil
}

func WriteOptions(current FileState, expected, contentType string) (minio.PutObjectOptions, error) {
	if expected == "" || current.Revision != expected {
		return minio.PutObjectOptions{}, apperror.New(409, "DestinationChanged", messages.ObjectsDestinationChanged)
	}
	opts := minio.PutObjectOptions{ContentType: contentType, PartSize: 16 * 1024 * 1024, NumThreads: 1}
	if current.Exists {
		opts.SetMatchETag(current.ETag)
	} else {
		opts.SetMatchETagExcept("*")
	}
	return opts, nil
}

func FileWriteError(err error) error {
	if code := minio.ToErrorResponse(err).Code; code == "PreconditionFailed" || code == "ConditionalRequestConflict" {
		return apperror.New(409, "DestinationChanged", messages.ObjectsDestinationChangedConditional)
	}
	return err
}

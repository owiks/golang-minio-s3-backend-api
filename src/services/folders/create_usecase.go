package folders

import (
	"context"
	"strings"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
)

func CreateFolder(ctx context.Context, lock func(context.Context, string) (func(), error), s CreateFolderClient, body CreateFolderRequest) (CreatedFolder, error) {
	if body.Bucket == "" || len(body.Bucket) > 63 {
		return CreatedFolder{}, apperror.New(400, "InvalidFolderPath", "Укажите корректный бакет.")
	}
	key, err := FolderKey(body.Prefix, body.Name)
	if err != nil {
		return CreatedFolder{}, err
	}
	release, lockErr := lock(ctx, "bucket/"+body.Bucket)
	if lockErr != nil {
		return CreatedFolder{}, lockErr
	}
	defer release()
	exists, err := FolderExists(ctx, s, body.Bucket, key)
	if err != nil {
		return CreatedFolder{}, err
	}
	if exists {
		return CreatedFolder{}, FolderConflict()
	}
	opts := minio.PutObjectOptions{ContentType: "application/x-directory", DisableMultipart: true}
	// A concurrent create must not overwrite a marker or create another version.
	opts.SetMatchETagExcept("*")
	if _, err = s.PutObject(ctx, body.Bucket, key, strings.NewReader(""), 0, opts); err != nil {
		code := minio.ToErrorResponse(err).Code
		if code == "PreconditionFailed" || code == "ConditionalRequestConflict" {
			return CreatedFolder{}, FolderConflict()
		}
		return CreatedFolder{}, err
	}
	return CreatedFolder{Bucket: body.Bucket, Key: key}, nil
}

type CreateFolderRequest struct {
	Bucket string `json:"bucket"`
	Prefix string `json:"prefix"`
	Name   string `json:"name"`
}

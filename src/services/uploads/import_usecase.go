package uploads

import (
	"context"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	objectwrite "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/write"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/result"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/staging"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads/source"
	validationsvc "github.com/owiks/golang-minio-s3-backend-api/src/services/validation"
)

type ImportRequest struct {
	URL      string `json:"url"`
	Revision string `json:"revision"`
}

type PreparedImport struct {
	client                     ImportClient
	bucket, key, url, revision string
}

func PrepareImport(ctx context.Context, s ImportClient, bucket, key string, body ImportRequest) (*PreparedImport, error) {
	var err error
	if bucket == "" || len(bucket) > 63 || !validationsvc.ValidFileKey(key) {
		return nil, apperror.New(400, "InvalidKey", messages.UploadsInvalidKey)
	}
	if _, err = source.ParseURL(body.URL); err != nil {
		return nil, err
	}
	current, err := objectwrite.DestinationState(ctx, s, bucket, key)
	if err != nil {
		return nil, err
	}
	if _, err = objectwrite.WriteOptions(current, body.Revision, ""); err != nil {
		return nil, err
	}
	return &PreparedImport{client: s, bucket: bucket, key: key, url: body.URL, revision: body.Revision}, nil
}
func Import(ctx context.Context, budget *staging.Budget, remote RemoteSource, job *PreparedImport, send func(result.StreamEvent[ImportResult])) (int64, error) {
	if job == nil || job.client == nil {
		return 0, apperror.New(400, "ImportNotPrepared", messages.UploadsImportNotPrepared)
	}
	s, bucket, key := job.client, job.bucket, job.key
	send(result.StreamEvent[ImportResult]{Progress: &result.Progress{Bytes: 0, Total: MaxRemoteSize}})
	f, size, contentType, e := StageRemote(ctx, remote, job.url, func(n, total int64) {
		send(result.StreamEvent[ImportResult]{Progress: &result.Progress{Bytes: n, Total: total}})
	}, budget)
	if e == nil {
		defer f.Close()
		var optsErr error
		current, optsErr := objectwrite.DestinationState(ctx, s, bucket, key)
		e = optsErr
		if e == nil {
			opts, oe := objectwrite.WriteOptions(current, job.revision, contentType)
			e = oe
			if e == nil {
				_, e = s.PutObject(ctx, bucket, key, f, size, opts)
			}
		}
	}
	return size, objectwrite.FileWriteError(e)
}

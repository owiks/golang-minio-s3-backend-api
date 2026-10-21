package archives

import (
	"context"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/coordination"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	objectssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
)

func ReadGzip(ctx context.Context, admission coordination.TransferAdmission, s InspectZIPClient, bucket, key string) (GzipContent, error) {
	release, admissionErr := admission.AcquireTransfer(s.SessionID())
	if admissionErr != nil {
		return GzipContent{}, admissionErr
	}
	defer release()
	bucket, key, e := objectssvc.ObjectParams(bucket, key)
	if e != nil {
		return GzipContent{}, e
	}
	if !strings.HasSuffix(strings.ToLower(key), ".gz") {
		return GzipContent{}, apperror.New(415, "NotGzip", messages.ArchivesNotGzip)
	}
	info, e := s.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if e != nil {
		return GzipContent{}, e
	}
	if info.Size > MaxGzipInput {
		return GzipContent{}, apperror.New(413, "GzipTooLarge", messages.ArchivesGzipTooLarge)
	}
	opts := minio.GetObjectOptions{}
	_ = opts.SetMatchETag(info.ETag)
	obj, e := s.GetObject(ctx, bucket, key, opts)
	if e != nil {
		return GzipContent{}, e
	}
	defer obj.Close()
	data, e := io.ReadAll(io.LimitReader(obj, MaxGzipInput+1))
	if e != nil {
		return GzipContent{}, e
	}
	if int64(len(data)) != info.Size {
		return GzipContent{}, apperror.New(409, "ObjectChanged", messages.ArchivesObjectChanged)
	}
	text, e := DecodeGzipText(ctx, data)
	if e != nil {
		return GzipContent{}, e
	}
	return GzipContent{Text: text, Compressed: len(data), Expanded: len(text)}, nil
}

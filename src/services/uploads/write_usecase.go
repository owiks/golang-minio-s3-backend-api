package uploads

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/objects/write"
	validationsvc "minio-web/api/src/services/validation"
)

func PrepareFile(ctx context.Context, s objectwrite.StateReader, bucket, key string) (objectwrite.FileState, error) {
	if bucket == "" || len(bucket) > 63 || !validationsvc.ValidFileKey(key) {
		return objectwrite.FileState{}, apperror.New(400, "InvalidFileKey", "Некорректный путь файла.")
	}
	return objectwrite.DestinationState(ctx, s, bucket, key)
}
func WriteFile(ctx context.Context, s ImportClient, bucket, key, revision, contentType string, size int64, source io.Reader) (minio.UploadInfo, error) {
	return writeFile(ctx, s, bucket, key, revision, contentType, size, source, false)
}

func writeFile(ctx context.Context, s ImportClient, bucket, key, revision, contentType string, size int64, source io.Reader, protect bool) (minio.UploadInfo, error) {
	if size < 0 || size > objectwrite.MaxFileSize {
		return minio.UploadInfo{}, apperror.New(400, "InvalidUploadSize", "Размер файла не должен превышать 5 ГиБ.")
	}
	current, err := PrepareFile(ctx, s, bucket, key)
	if err != nil {
		return minio.UploadInfo{}, err
	}
	opts, err := objectwrite.WriteOptions(current, revision, contentType)
	if err != nil {
		return minio.UploadInfo{}, err
	}
	// Optional integrity uploads are preverified on bounded temporary storage.
	// Native Content-MD5 then protects each actual HTTP PUT, including multipart
	// parts, without requiring global SDK trailer support.
	opts.SendContentMd5 = protect
	result, err := s.PutObject(ctx, bucket, key, source, size, opts)
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) || errors.Is(err, io.ErrUnexpectedEOF) {
		return minio.UploadInfo{}, apperror.New(400, "IncompleteUpload", "Файл передан не полностью. Проверьте состояние перед повтором.")
	}
	if code := minio.ToErrorResponse(err).Code; code == "BadDigest" || code == "InvalidDigest" || code == "XAmzContentSHA256Mismatch" {
		return minio.UploadInfo{}, apperror.New(400, "ChecksumMismatch", "Хранилище отклонило контрольную сумму. Проверьте состояние файла перед повтором.")
	}
	return result, objectwrite.FileWriteError(err)
}

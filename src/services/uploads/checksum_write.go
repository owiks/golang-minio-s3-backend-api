package uploads

import (
	"context"
	"errors"
	"io"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	objectwrite "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/write"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/staging"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads/integrity"
)

// WriteFileWithChecksum preserves the existing write path when no checksum is
// requested. With an expected full-object checksum it stages and verifies all
// bytes before any upload command; destination preconditions are rechecked after
// staging and still applied natively to the actual write/complete.
func WriteFileWithChecksum(ctx context.Context, budget *staging.Budget, client ImportClient, bucket, key, revision, contentType string, size int64, source io.Reader, expected *integrity.Spec) (UploadReceipt, error) {
	if expected == nil {
		info, err := WriteFile(ctx, client, bucket, key, revision, contentType, size, source)
		if err != nil {
			return UploadReceipt{}, err
		}
		return receipt(info, nil), nil
	}
	spec, err := integrity.Normalize(*expected, integrity.Object)
	if err != nil {
		return UploadReceipt{}, err
	}
	if spec.Type != integrity.FullObject {
		return UploadReceipt{}, integrity.UnsupportedMode()
	}
	if size < 0 || size > objectwrite.MaxFileSize {
		return UploadReceipt{}, apperror.New(400, "InvalidUploadSize", messages.UploadsInvalidUploadSize)
	}
	current, err := PrepareFile(ctx, client, bucket, key)
	if err != nil {
		return UploadReceipt{}, err
	}
	if _, err = objectwrite.WriteOptions(current, revision, contentType); err != nil {
		return UploadReceipt{}, err
	}
	if budget == nil {
		return UploadReceipt{}, apperror.New(503, "ChecksumStagingUnavailable", messages.UploadsChecksumStagingUnavailable)
	}
	file, err := budget.Create(size, "minio-checksum-upload-*")
	if err != nil {
		return UploadReceipt{}, err
	}
	defer file.Close() // The shared budget retains failed unlink for bounded retry.
	stage := &checksumStageWriter{file: file}
	computed, err := integrity.Hash(ctx, io.TeeReader(io.LimitReader(source, size), stage), size, spec)
	if err != nil {
		if stage.err != nil {
			return UploadReceipt{}, stage.err
		}
		return UploadReceipt{}, uploadBodyError(ctx, err)
	}
	if err = checkEnd(ctx, source); err != nil {
		return UploadReceipt{}, err
	}
	if err = integrity.Compare(spec, computed); err != nil {
		return UploadReceipt{}, err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return UploadReceipt{}, err
	}
	info, err := writeFile(ctx, client, bucket, key, revision, contentType, size, file, true)
	if err != nil {
		return UploadReceipt{}, err
	}
	protection := "CONTENT_MD5"
	if size > 16*1024*1024 {
		protection = "PART_CONTENT_MD5"
	}
	evidence := integrity.FromUpload(info, &spec, &computed, protection)
	value := receipt(info, evidence)
	if evidence.Verification == "storage_mismatch" {
		value.OK = false
		value.WriteState = "committed"
	}
	return value, nil
}

type checksumStageWriter struct {
	file *staging.File
	err  error
}

func (s *checksumStageWriter) Write(data []byte) (int, error) {
	n, err := s.file.Write(data)
	s.err = err
	return n, err
}

func receipt(info minio.UploadInfo, evidence *integrity.Evidence) UploadReceipt {
	if evidence == nil {
		observed := integrity.FromUpload(info, nil, nil, "")
		if len(observed.Storage) > 0 {
			evidence = observed
		}
	}
	return UploadReceipt{OK: true, ETag: info.ETag, VersionID: info.VersionID, Size: info.Size, Checksum: evidence}
}

func checkEnd(ctx context.Context, source io.Reader) error {
	var one [1]byte
	for attempts := 0; attempts < 100; attempts++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := source.Read(one[:])
		if n > 0 {
			return incompleteBody()
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return uploadBodyError(ctx, err)
		}
	}
	return incompleteBody()
}

func incompleteBody() error {
	return apperror.New(400, "IncompleteUpload", messages.UploadSizeMismatch)
}

func uploadBodyError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var issue *apperror.Error
	if errors.As(err, &issue) {
		return err
	}
	return apperror.NewWithCause(400, "IncompleteUpload", messages.UploadInterrupted, err)
}

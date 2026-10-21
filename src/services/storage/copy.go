package storage

import (
	"context"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/s3utils"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
)

type CopyRequest struct{ SourceBucket, SourceKey, DestinationBucket, DestinationKey string }

// CopyConditional preserves conditional completion. CopyObject has no destination
// preconditions in this SDK; multipart copy does, so a racing PUT cannot be lost.
// The boolean is false only when the endpoint explicitly rejects initiation or
// part-copy and an initiated upload has been successfully aborted.
func CopyConditional(ctx context.Context, client CopyClient, body CopyRequest, source minio.ObjectInfo, opts minio.PutObjectOptions, partSize int64) (minio.UploadInfo, bool, error) {
	if source.Size == 0 {
		get := minio.GetObjectOptions{VersionID: source.VersionID}
		if err := get.SetMatchETag(source.ETag); err != nil {
			return minio.UploadInfo{}, true, err
		}
		object, err := client.GetObject(ctx, body.SourceBucket, body.SourceKey, get)
		if err != nil {
			return minio.UploadInfo{}, true, copySourceError(err)
		}
		defer object.Close()
		var first [1]byte
		n, readErr := object.Read(first[:])
		if n != 0 || readErr == nil {
			return minio.UploadInfo{}, true, copySourceError(minio.ErrorResponse{Code: "PreconditionFailed"})
		}
		if readErr != io.EOF {
			return minio.UploadInfo{}, true, copySourceError(readErr)
		}
		result, err := client.PutObject(ctx, body.DestinationBucket, body.DestinationKey, strings.NewReader(""), 0, opts)
		return result, true, err
	}
	core := client
	id, err := core.NewMultipartUpload(ctx, body.DestinationBucket, body.DestinationKey, opts)
	if err != nil {
		if unsupportedCopy(err) {
			return minio.UploadInfo{}, false, nil
		}
		return minio.UploadInfo{}, true, err
	}
	if id == "" {
		return minio.UploadInfo{}, true, apperror.New(502, "CopyUnconfirmed", messages.StorageCopyUnconfirmed)
	}
	committed := false
	aborted := false
	abort := func() error {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		err := core.AbortMultipartUpload(cleanup, body.DestinationBucket, body.DestinationKey, id)
		aborted = err == nil
		if err != nil {
			telemetry.Event(ctx, "copy multipart cleanup unconfirmed")
		}
		return err
	}
	defer func() {
		if !committed && !aborted {
			_ = abort()
		}
	}()
	src := s3utils.EncodePath(body.SourceBucket + "/" + body.SourceKey)
	if source.VersionID != "" {
		src += "?versionId=" + url.QueryEscape(source.VersionID)
	}
	headers := map[string]string{"x-amz-copy-source": src, "x-amz-copy-source-if-match": `"` + source.ETag + `"`}
	parts := []minio.CompletePart{}
	if partSize < 5*1024*1024 {
		return minio.UploadInfo{}, true, apperror.New(500, "InvalidCopyPartSize", messages.StorageInvalidCopyPartSize)
	}
	if needed := (source.Size + 9998) / 9999; needed > partSize {
		partSize = (needed + 1024*1024 - 1) / (1024 * 1024) * (1024 * 1024)
	}
	if partSize > 5*1024*1024*1024 {
		return minio.UploadInfo{}, true, apperror.New(400, "ObjectTooLarge", messages.StorageObjectTooLarge)
	}
	for offset := int64(0); offset < source.Size; offset += partSize {
		part, err := core.CopyObjectPart(ctx, body.SourceBucket, body.SourceKey, body.DestinationBucket, body.DestinationKey, id, len(parts)+1, offset, min(partSize, source.Size-offset), headers)
		if err != nil {
			if code := minio.ToErrorResponse(err).Code; code == "PreconditionFailed" || code == "ConditionalRequestConflict" {
				return minio.UploadInfo{}, true, apperror.New(409, "SourceChanged", messages.StorageSourceChangedCopy)
			}
			if unsupportedCopy(err) && abort() == nil {
				return minio.UploadInfo{}, false, nil
			}
			return minio.UploadInfo{}, true, err
		}
		if part.ETag == "" {
			return minio.UploadInfo{}, true, apperror.New(502, "CopyPartUnconfirmed", messages.StorageCopyPartUnconfirmed)
		}
		parts = append(parts, part)
	}
	result, err := core.CompleteMultipartUpload(ctx, body.DestinationBucket, body.DestinationKey, id, parts, opts)
	if err != nil {
		return minio.UploadInfo{}, true, err
	}
	if result.ETag == "" {
		return minio.UploadInfo{}, true, apperror.New(502, "CopyUnconfirmed", messages.StorageCopyUnconfirmedCopy)
	}
	committed = true
	return result, true, nil
}
func unsupportedCopy(err error) bool {
	switch minio.ToErrorResponse(err).Code {
	case "NotImplemented", "XNotImplemented", "UnsupportedOperation":
		return true
	}
	return false
}

func copySourceError(err error) error {
	switch minio.ToErrorResponse(err).Code {
	case "NoSuchKey", "NoSuchVersion", "PreconditionFailed", "ConditionalRequestConflict":
		return apperror.New(409, "SourceChanged", messages.StorageSourceChangedCopySourceError)
	}
	return err
}

package storage

import (
	"context"
	"io"
	"net/http"
	"net/url"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

type conditionalUploadKey struct{}
type conditionalUpload struct {
	bucket, key           string
	matchETag, exceptETag string
}

// PutObject retains the caller's native destination conditions across the SDK's
// high-level multipart path. v7.4.1's completeUploadOpts drops custom headers;
// initiation conditions alone do not protect the final object commit.
func (c *S3Client) PutObject(ctx context.Context, bucket, key string, source io.Reader, size int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	headers := opts.Header()
	condition := conditionalUpload{bucket: bucket, key: key, matchETag: headers.Get("If-Match"), exceptETag: headers.Get("If-None-Match")}
	if condition.matchETag != "" || condition.exceptETag != "" {
		ctx = context.WithValue(ctx, conditionalUploadKey{}, condition)
	}
	return c.Client.PutObject(ctx, bucket, key, source, size, opts)
}

// restoreConditionalComplete touches only the exact SDK-generated final request
// belonging to this PutObject call. It cannot affect another key, initiation,
// parts, aborts or future/unknown subresources. The SDK already signed this
// request: added If-* headers remain native preconditions but are not added to
// SignedHeaders. Core.CompleteMultipartUpload keeps and signs its own options.
func restoreConditionalComplete(ctx context.Context, method string, target *url.URL, headers http.Header) error {
	condition, ok := ctx.Value(conditionalUploadKey{}).(conditionalUpload)
	if !ok || method != http.MethodPost || target == nil || target.Path != "/"+condition.bucket+"/"+condition.key {
		return nil
	}
	query, err := url.ParseQuery(target.RawQuery)
	if err != nil || len(query) != 1 || len(query["uploadId"]) != 1 || query.Get("uploadId") == "" {
		return nil
	}
	for _, field := range []struct{ name, value string }{{"If-Match", condition.matchETag}, {"If-None-Match", condition.exceptETag}} {
		current := headers.Get(field.name)
		if current != "" && current != field.value {
			return apperror.New(502, "ConditionalUploadConflict", messages.StorageConditionalUploadConflict)
		}
		if field.value != "" {
			headers.Set(field.name, field.value)
		}
	}
	return nil
}

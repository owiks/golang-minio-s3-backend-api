package uploads

import (
	"context"
	"github.com/minio/minio-go/v7"
	"io"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/result"
	"minio-web/api/src/services/staging"
	"minio-web/api/src/services/uploads/source"
	"strings"
	"testing"
)

type importDestination struct {
	conditionalDestination
	bucket, key string
}

func (s *importDestination) PutObject(ctx context.Context, bucket, key string, r io.Reader, size int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	s.bucket, s.key = bucket, key
	return s.conditionalDestination.PutObject(ctx, bucket, key, r, size, opts)
}
func TestPreparedImportOwnsValidatedSourceDestinationAndRevision(t *testing.T) {
	client := &importDestination{conditionalDestination: conditionalDestination{lookupErr: minio.ErrorResponse{Code: "NoSuchKey"}}}
	body := ImportRequest{URL: "https://example.org/original", Revision: "absent"}
	job, err := PrepareImport(context.Background(), client, "test-bucket", "folder/file", body)
	if err != nil {
		t.Fatal(err)
	}
	body.URL = "https://example.org/other"
	body.Revision = "changed"
	remote := importSourceFunc(func(_ context.Context, raw string) (source.File, error) {
		if raw != "https://example.org/original" {
			t.Fatal("source changed", raw)
		}
		return source.File{Body: io.NopCloser(strings.NewReader("body")), Size: 4}, nil
	})
	budget := staging.New(4)
	defer budget.Close()
	_, err = Import(context.Background(), budget, remote, job, func(result.StreamEvent[ImportResult]) {})
	if err != nil || client.bucket != "test-bucket" || client.key != "folder/file" || client.writes != 1 || client.headers.Get("If-None-Match") != "*" {
		t.Fatal("prepared scope lost", client, err)
	}
	for _, bad := range []*PreparedImport{nil, {}} {
		_, err = Import(context.Background(), budget, remote, bad, func(result.StreamEvent[ImportResult]) {})
		if err == nil || apperror.Public(err).Code != "ImportNotPrepared" {
			t.Fatal("unprepared import accepted", err)
		}
	}
}

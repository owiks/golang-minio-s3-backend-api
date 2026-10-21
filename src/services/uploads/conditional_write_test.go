package uploads

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/objects/write"
	"minio-web/api/src/services/result"
	"minio-web/api/src/services/staging"
	"minio-web/api/src/services/uploads/source"
)

type conditionalDestination struct {
	info                minio.ObjectInfo
	lookupErr, writeErr error
	lookups, writes     int
	headers             http.Header
	body                string
}

func (s *conditionalDestination) StatObject(ctx context.Context, bucket, key string, _ minio.StatObjectOptions) (minio.ObjectInfo, error) {
	s.lookups++
	if err := ctx.Err(); err != nil {
		return minio.ObjectInfo{}, err
	}
	return s.info, s.lookupErr
}

func (s *conditionalDestination) PutObject(ctx context.Context, bucket, key string, source io.Reader, size int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	s.writes++
	s.headers = opts.Header()
	data, err := io.ReadAll(source)
	s.body = string(data)
	if err != nil {
		return minio.UploadInfo{}, err
	}
	return minio.UploadInfo{Size: size, ETag: "written"}, s.writeErr
}

func TestWriteFileNeverDowngradesConditionalFailure(t *testing.T) {
	for _, scenario := range []string{"create", "overwrite", "stale", "unconfirmed", "lookup-denied", "canceled", "native-precondition", "native-conflict", "write-denied", "incomplete"} {
		t.Run(scenario, func(t *testing.T) {
			client := &conditionalDestination{lookupErr: minio.ErrorResponse{Code: "NoSuchKey"}}
			revision := "absent"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantWrites, wantCode := 1, ""
			switch scenario {
			case "overwrite":
				client.lookupErr = nil
				client.info = minio.ObjectInfo{ETag: "before", Size: 3}
				state, err := objectwrite.DestinationState(ctx, client, "bucket", "file.txt")
				if err != nil {
					t.Fatal(err)
				}
				revision = state.Revision
			case "stale":
				revision, wantWrites, wantCode = "stale", 0, "DestinationChanged"
			case "unconfirmed":
				revision, wantWrites, wantCode = "", 0, "DestinationChanged"
			case "lookup-denied":
				client.lookupErr = minio.ErrorResponse{Code: "AccessDenied"}
				wantWrites, wantCode = 0, "AccessDenied"
			case "canceled":
				cancel()
				wantWrites = 0
			case "native-precondition":
				client.writeErr = minio.ErrorResponse{Code: "PreconditionFailed"}
				wantCode = "DestinationChanged"
			case "native-conflict":
				client.writeErr = minio.ErrorResponse{Code: "ConditionalRequestConflict"}
				wantCode = "DestinationChanged"
			case "write-denied":
				client.writeErr = minio.ErrorResponse{Code: "AccessDenied"}
				wantCode = "AccessDenied"
			case "incomplete":
				client.writeErr = io.ErrUnexpectedEOF
				wantCode = "IncompleteUpload"
			}
			receipt, err := WriteFile(ctx, client, "bucket", "file.txt", revision, "text/plain", 4, strings.NewReader("body"))
			if client.writes != wantWrites {
				t.Fatal("unexpected write or retry", client.writes, wantWrites)
			}
			if scenario == "canceled" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation lost", err)
				}
			} else if wantCode != "" {
				if err == nil || apperror.Public(err).Code != wantCode {
					t.Fatal("write error changed", err, wantCode)
				}
			} else if err != nil || receipt.Size != 4 {
				t.Fatal("conditional write failed", receipt, err)
			}
			if wantWrites == 1 {
				if client.body != "body" || client.headers.Get("Content-Type") != "text/plain" {
					t.Fatal("payload or content type changed")
				}
				if scenario == "overwrite" {
					if client.headers.Get("If-Match") != `"before"` || client.headers.Get("If-None-Match") != "" {
						t.Fatal("overwrite condition lost", client.headers)
					}
				} else if client.headers.Get("If-None-Match") != "*" || client.headers.Get("If-Match") != "" {
					t.Fatal("create condition lost", client.headers)
				}
			}
		})
	}
}

type importSourceFunc func(context.Context, string) (source.File, error)

func (f importSourceFunc) Open(ctx context.Context, raw string) (source.File, error) {
	return f(ctx, raw)
}

func TestImportRechecksDestinationAfterStaging(t *testing.T) {
	for _, scenario := range []string{"create", "changed-during-fetch", "denied-after-fetch", "native-conflict"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			destination := &conditionalDestination{lookupErr: minio.ErrorResponse{Code: "NoSuchKey"}}
			job, err := PrepareImport(ctx, destination, "bucket", "file.txt", ImportRequest{URL: "https://example.org/file.txt", Revision: "absent"})
			if err != nil || destination.lookups != 1 {
				t.Fatal("initial destination check failed", err)
			}
			fetches := 0
			client := importSourceFunc(func(context.Context, string) (source.File, error) {
				fetches++
				if destination.lookups != 1 || destination.writes != 0 {
					t.Fatal("import writes before source staging")
				}
				switch scenario {
				case "changed-during-fetch":
					destination.lookupErr = nil
					destination.info = minio.ObjectInfo{ETag: "concurrent-writer", Size: 8}
				case "denied-after-fetch":
					destination.lookupErr = minio.ErrorResponse{Code: "AccessDenied"}
				case "native-conflict":
					destination.writeErr = minio.ErrorResponse{Code: "PreconditionFailed"}
				}
				return source.File{Size: 4, Body: io.NopCloser(strings.NewReader("body")), ContentType: "text/plain"}, nil
			})
			budget := staging.New(4)
			_, err = Import(ctx, budget, client, job, func(result.StreamEvent[ImportResult]) {})
			wantWrites, wantCode := 1, ""
			switch scenario {
			case "changed-during-fetch":
				wantWrites, wantCode = 0, "DestinationChanged"
			case "denied-after-fetch":
				wantWrites, wantCode = 0, "AccessDenied"
			case "native-conflict":
				wantCode = "DestinationChanged"
			}
			if fetches != 1 || destination.lookups != 2 || destination.writes != wantWrites || budget.Used() != 0 {
				t.Fatal("import check order, retries or staging cleanup changed", fetches, destination.lookups, destination.writes, budget.Used())
			}
			if wantCode == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || apperror.Public(err).Code != wantCode {
				t.Fatal("import conflict mapping changed", err, wantCode)
			}
			if wantWrites == 1 && destination.headers.Get("If-None-Match") != "*" {
				t.Fatal("import lost create-only condition")
			}
		})
	}
}

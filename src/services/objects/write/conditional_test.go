package objectwrite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	objectstate "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/state"
)

type stateReaderFunc func(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)

func (f stateReaderFunc) StatObject(ctx context.Context, bucket, key string, opts minio.StatObjectOptions) (minio.ObjectInfo, error) {
	return f(ctx, bucket, key, opts)
}

func TestDestinationSnapshotAndNativeConditions(t *testing.T) {
	info := minio.ObjectInfo{ETag: "original-etag", Size: 123, LastModified: time.Date(2026, 10, 20, 1, 2, 3, 0, time.UTC), VersionID: "original-version"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state, err := DestinationState(ctx, stateReaderFunc(func(got context.Context, bucket, key string, opts minio.StatObjectOptions) (minio.ObjectInfo, error) {
		if got != ctx || bucket != "bucket" || key != "folder/file.txt" || opts.VersionID != "" {
			t.Fatal("destination lookup changed scope")
		}
		return info, nil
	}), "bucket", "folder/file.txt")
	wantRevision := objectstate.DeletionRevision(objectstate.DeletionSnapshot{ETag: info.ETag, Size: info.Size, Modified: info.LastModified, Version: info.VersionID})
	if err != nil || !state.Exists || state.Revision != wantRevision || state.ETag != info.ETag || state.Size != info.Size {
		t.Fatal("destination snapshot changed", state, err)
	}
	opts, err := WriteOptions(state, wantRevision, "text/plain")
	if err != nil || opts.Header().Get("If-Match") != `"original-etag"` || opts.Header().Get("If-None-Match") != "" {
		t.Fatal("overwrite lost native condition", opts.Header(), err)
	}
	if opts.ContentType != "text/plain" || opts.PartSize != 16*1024*1024 || opts.NumThreads != 1 {
		t.Fatal("write settings changed")
	}
	// Each source-state component contributes to the confirmation token.
	for _, field := range []string{"etag", "size", "modified", "version"} {
		t.Run(field, func(t *testing.T) {
			changed := info
			switch field {
			case "etag":
				changed.ETag += "-new"
			case "size":
				changed.Size++
			case "modified":
				changed.LastModified = changed.LastModified.Add(time.Nanosecond)
			case "version":
				changed.VersionID += "-new"
			}
			current, err := DestinationState(ctx, stateReaderFunc(func(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error) {
				return changed, nil
			}), "bucket", "folder/file.txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = WriteOptions(current, wantRevision, "text/plain"); err == nil || apperror.Public(err).Code != "DestinationChanged" {
				t.Fatal("stale destination accepted", err)
			}
		})
	}
}

func TestOnlyMissingKeyAllowsCreate(t *testing.T) {
	for _, code := range []string{"NoSuchKey", "NoSuchBucket", "AccessDenied", "NotFound", "InternalError"} {
		t.Run(code, func(t *testing.T) {
			failure := minio.ErrorResponse{Code: code}
			state, err := DestinationState(context.Background(), stateReaderFunc(func(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error) {
				return minio.ObjectInfo{}, failure
			}), "bucket", "file.txt")
			if code != "NoSuchKey" {
				if err != failure || state != (FileState{}) {
					t.Fatal("failed lookup treated as absent", state, err)
				}
				return
			}
			if err != nil || state != (FileState{Revision: "absent"}) {
				t.Fatal("missing key snapshot changed", state, err)
			}
			opts, err := WriteOptions(state, "absent", "application/json")
			if err != nil || opts.Header().Get("If-None-Match") != "*" || opts.Header().Get("If-Match") != "" {
				t.Fatal("create is not conditional", opts.Header(), err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := DestinationState(ctx, stateReaderFunc(func(ctx context.Context, _ string, _ string, _ minio.StatObjectOptions) (minio.ObjectInfo, error) {
		return minio.ObjectInfo{}, ctx.Err()
	}), "bucket", "file.txt")
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}

func TestUnconfirmedWriteRejected(t *testing.T) {
	for _, state := range []FileState{{Revision: "absent"}, {Exists: true, Revision: "current", ETag: "etag"}} {
		for _, expected := range []string{"", "stale"} {
			if _, err := WriteOptions(state, expected, "text/plain"); err == nil || apperror.Public(err).Status != 409 || apperror.Public(err).Code != "DestinationChanged" {
				t.Fatal("unconfirmed write accepted", state, expected, err)
			}
		}
	}
}

func TestNativeWriteConflictMapping(t *testing.T) {
	for _, code := range []string{"PreconditionFailed", "ConditionalRequestConflict"} {
		mapped := FileWriteError(minio.ErrorResponse{Code: code})
		if got := apperror.Public(mapped); got.Status != 409 || got.Code != "DestinationChanged" {
			t.Fatal("native conflict not mapped", got)
		}
	}
	for _, failure := range []error{nil, context.Canceled, context.DeadlineExceeded, errors.New("transport failed"), minio.ErrorResponse{Code: "AccessDenied"}, apperror.New(409, "SourceChanged", messages.Literal("source changed"))} {
		if FileWriteError(failure) != failure {
			t.Fatal("non-destination error replaced", failure)
		}
	}
}

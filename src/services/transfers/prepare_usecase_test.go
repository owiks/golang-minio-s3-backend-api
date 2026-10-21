package transfers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	folderssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/folders"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/objects/write"
)

type destinationReaderFunc func(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)

func (f destinationReaderFunc) StatObject(ctx context.Context, bucket, key string, opts minio.StatObjectOptions) (minio.ObjectInfo, error) {
	return f(ctx, bucket, key, opts)
}

type folderReaderFunc func(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error)

func (f folderReaderFunc) ListObjectPage(ctx context.Context, bucket, prefix, token, delimiter string, limit int) (minio.ListBucketV2Result, error) {
	return f(ctx, bucket, prefix, token, delimiter, limit)
}

func TestPrepareDestinationValidatesBeforeReading(t *testing.T) {
	for _, tc := range []struct{ bucket, key, code string }{
		{"", "file.txt", "InvalidObject"},
		{strings.Repeat("b", 64), "file.txt", "InvalidObject"},
		{"bucket", "", "InvalidObject"},
		{"bucket", strings.Repeat("a", 1025), "InvalidObject"},
		{"bucket", "/", "InvalidTransfer"},
		{"bucket", "a/../file.txt", "InvalidTransfer"},
		{"bucket", "a\x00.txt", "InvalidTransfer"},
		{"bucket", "a\\file.txt", "InvalidTransfer"},
		{"bucket", "\xff", "InvalidTransfer"},
	} {
		state, err := PrepareDestination(context.Background(), nil, tc.bucket, tc.key)
		if err == nil || apperror.Public(err).Status != 400 || apperror.Public(err).Code != tc.code || state != (objectwrite.FileState{}) {
			t.Fatalf("invalid destination accepted: %+v, %v", tc, err)
		}
	}
}

func TestPrepareDestinationPreservesScopeAndReadErrors(t *testing.T) {
	for _, key := range []string{"folder/file.txt", "папка + # %/", "opaque//file.txt"} {
		ctx, cancel := context.WithCancel(context.Background())
		for _, failure := range []error{nil, minio.ErrorResponse{Code: "NoSuchKey"}, minio.ErrorResponse{Code: "NoSuchBucket"}, minio.ErrorResponse{Code: "AccessDenied"}, context.Canceled} {
			calls := 0
			reader := destinationReaderFunc(func(got context.Context, bucket, gotKey string, opts minio.StatObjectOptions) (minio.ObjectInfo, error) {
				calls++
				if got != ctx || bucket != "bucket" || gotKey != key || opts.VersionID != "" {
					t.Fatal("destination scope changed")
				}
				return minio.ObjectInfo{ETag: "etag", Size: 7, VersionID: "version"}, failure
			})
			state, err := PrepareDestination(ctx, reader, "bucket", key)
			if calls != 1 {
				t.Fatal("destination read retried")
			}
			switch {
			case failure == nil:
				if err != nil || !state.Exists || state.Revision == "" || state.ETag != "etag" || state.Size != 7 {
					t.Fatal("destination state lost", state, err)
				}
			case minio.ToErrorResponse(failure).Code == "NoSuchKey":
				if err != nil || state != (objectwrite.FileState{Revision: "absent"}) {
					t.Fatal("missing destination not recognized", state, err)
				}
			default:
				if err != failure || state != (objectwrite.FileState{}) {
					t.Fatal("read failure became usable destination state", state, err)
				}
			}
		}
		cancel()
	}
}

func TestPrepareFolderRejectsInvalidScopeBeforeListing(t *testing.T) {
	for _, options := range []PrepareFolderOptions{
		{Bucket: "bucket", Prefix: "folder/", VersionRequested: true},
		{Bucket: "bucket", Prefix: ""},
		{Bucket: "", Prefix: "folder/"},
		{Bucket: strings.Repeat("b", 64), Prefix: "folder/"},
		{Bucket: "bucket", Prefix: "folder"},
		{Bucket: "bucket", Prefix: "a\x00/"},
		{Bucket: "bucket", Prefix: "\xff/"},
		{Bucket: "bucket", Prefix: strings.Repeat("a", 1024) + "/"},
	} {
		value, err := PrepareFolderTransfer(context.Background(), nil, options)
		if err == nil || apperror.Public(err).Code != "InvalidFolderDelete" || apperror.Public(err).Status != 400 || value.Items != nil {
			t.Fatal("invalid folder scope accepted", options, value, err)
		}
	}
}

func TestPrepareFolderPreservesOpaquePrefixPaginationAndMarkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prefix := "папка + # %//"
	pages := []minio.ListBucketV2Result{
		{Contents: []minio.ObjectInfo{{Key: prefix}, {Key: prefix + "file.txt", Size: 3}}, IsTruncated: true, NextContinuationToken: "opaque+/="},
		{Contents: []minio.ObjectInfo{{Key: prefix + "file.txt", Size: 3}, {Key: prefix + "nested/"}, {Key: prefix + "nested/file", Size: 5}}},
	}
	calls := 0
	value, err := PrepareFolderTransfer(ctx, folderReaderFunc(func(got context.Context, bucket, gotPrefix, token, delimiter string, limit int) (minio.ListBucketV2Result, error) {
		wantToken := ""
		if calls == 1 {
			wantToken = "opaque+/="
		}
		if got != ctx || bucket != "bucket" || gotPrefix != prefix || token != wantToken || delimiter != "" || limit != 500 || calls >= len(pages) {
			t.Fatal("listing scope/cursor changed", calls, bucket, gotPrefix, token, delimiter, limit)
		}
		page := pages[calls]
		calls++
		return page, nil
	}), PrepareFolderOptions{Bucket: "bucket", Prefix: prefix})
	want := []folderssvc.FolderDeleteItem{{Key: prefix, Marker: true}, {Key: prefix + "file.txt", Size: 3}, {Key: prefix + "nested/", Marker: true}, {Key: prefix + "nested/file", Size: 5}}
	if err != nil || calls != 2 || !reflect.DeepEqual(value.Items, want) {
		t.Fatal("complete manifest changed", value, err)
	}
}

func TestPrepareFolderNeverReturnsPartialPlanOnFailure(t *testing.T) {
	for _, scenario := range []string{"outside-prefix", "missing-cursor", "repeated-cursor", "denied", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := error(minio.ErrorResponse{Code: "AccessDenied"})
			calls := 0
			value, err := PrepareFolderTransfer(ctx, folderReaderFunc(func(ctx context.Context, _, _, _, _ string, _ int) (minio.ListBucketV2Result, error) {
				calls++
				if calls == 1 {
					return minio.ListBucketV2Result{Contents: []minio.ObjectInfo{{Key: "a/file"}}, IsTruncated: true, NextContinuationToken: "next"}, nil
				}
				if calls > 2 {
					t.Fatal("failed listing retried")
				}
				switch scenario {
				case "outside-prefix":
					return minio.ListBucketV2Result{Contents: []minio.ObjectInfo{{Key: "a-backup/keep"}}}, nil
				case "missing-cursor":
					return minio.ListBucketV2Result{IsTruncated: true}, nil
				case "repeated-cursor":
					return minio.ListBucketV2Result{IsTruncated: true, NextContinuationToken: "next"}, nil
				case "canceled":
					cancel()
					failure = ctx.Err()
				}
				return minio.ListBucketV2Result{}, failure
			}), PrepareFolderOptions{Bucket: "bucket", Prefix: "a/"})
			if err == nil || value.Items != nil || calls != 2 {
				t.Fatal("incomplete listing became a transfer plan", value, err, calls)
			}
			if scenario == "denied" || scenario == "canceled" {
				if !errors.Is(err, failure) {
					t.Fatal("upstream error replaced", err)
				}
			} else if apperror.Public(err).Code != "InvalidFolderListing" {
				t.Fatal("listing safety error changed", err)
			}
		})
	}
}

func TestPrepareFolderLimitAndEmptyManifest(t *testing.T) {
	for _, count := range []int{0, folderssvc.MaxFolderDeleteObjects, folderssvc.MaxFolderDeleteObjects + 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			items := make([]minio.ObjectInfo, count)
			for i := range items {
				items[i] = minio.ObjectInfo{Key: fmt.Sprintf("folder/%d", i), Size: 1}
			}
			value, err := PrepareFolderTransfer(context.Background(), folderReaderFunc(func(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error) {
				return minio.ListBucketV2Result{Contents: items}, nil
			}), PrepareFolderOptions{Bucket: "bucket", Prefix: "folder/"})
			if count > folderssvc.MaxFolderDeleteObjects {
				if err == nil || apperror.Public(err).Code != "TransferPlanLimit" || apperror.Public(err).Status != 400 || value.Items != nil {
					t.Fatal("transfer limit changed", value, err)
				}
			} else if err != nil || value.Items == nil || len(value.Items) != count {
				t.Fatal("valid or empty plan rejected", value, err)
			}
		})
	}
}

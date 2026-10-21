package objects

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

type versionReader func(context.Context, string, string, string, string, int) (storage.VersionPage, error)

func (f versionReader) ReadVersionPage(ctx context.Context, b, p, k, v string, n int) (storage.VersionPage, error) {
	return f(ctx, b, p, k, v, n)
}

func TestListVersionsUsesOperationPortAndNativeMarkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	key, calls := "folder/a +%.txt", 0
	reader := versionReader(func(got context.Context, bucket, prefix, km, vm string, n int) (storage.VersionPage, error) {
		calls++
		if got != ctx || bucket != "test-bucket" || prefix != key || n != 50 {
			t.Fatal("version request lost context or listing scope")
		}
		if calls == 2 {
			if km != key || vm != "native-version" {
				t.Fatal("native continuation markers lost", km, vm)
			}
			return storage.VersionPage{}, context.Canceled
		}
		if km != "" || vm != "" {
			t.Fatal("unexpected initial markers")
		}
		return storage.VersionPage{Rows: []storage.VersionRow{
			{Key: url.QueryEscape(key + "-other"), ID: "unrelated"},
			{Key: url.QueryEscape(key), ID: "deleted", Deleted: true, Latest: true},
		}, Truncated: true, KeyMarker: url.QueryEscape(key), VersionMarker: "native-version"}, nil
	})
	page, err := ListVersions(ctx, reader, "test-bucket", key, "")
	if err != nil || len(page.Rows) != 1 || page.Rows[0].Key != key || !page.Rows[0].Deleted || !page.Rows[0].Latest || page.Next == "" {
		t.Fatal("version filtering/decoding changed", page, err)
	}
	if _, err = ListVersions(ctx, reader, "other-bucket", key, page.Next); apperror.Public(err).Code != "InvalidCursor" || calls != 1 {
		t.Fatal("foreign cursor reached adapter", err)
	}
	if _, err = ListVersions(ctx, reader, "test-bucket", key, page.Next); !errors.Is(err, context.Canceled) {
		t.Fatal("adapter cancellation lost", err)
	}
	invalid := versionReader(func(context.Context, string, string, string, string, int) (storage.VersionPage, error) {
		return storage.VersionPage{}, storage.ErrInvalidVersionResponse
	})
	if _, err = ListVersions(ctx, invalid, "test-bucket", key, ""); apperror.Public(err).Code != "VersionListingFailed" {
		t.Fatal("malformed native error mapping changed", err)
	}
}

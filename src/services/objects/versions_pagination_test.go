package objects

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

func versionCursor(bucket, key, marker, version string) string {
	raw, _ := json.Marshal(struct{ Bucket, Key, Marker, Version string }{bucket, key, marker, version})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func TestVersionPaginationRejectsUnusableContinuationWithoutPartialRows(t *testing.T) {
	for _, tc := range []struct {
		name, marker, version, cursor string
		empty                         bool
	}{
		{name: "missing both markers"},
		{name: "missing key marker", version: "next"},
		{name: "missing version marker", marker: "file"},
		{name: "oversized key marker", marker: strings.Repeat("x", 4097), version: "next"},
		{name: "control in key marker", marker: "file\n", version: "next"},
		{name: "malformed key encoding", marker: "%XX", version: "next"},
		{name: "oversized version", marker: "file", version: strings.Repeat("x", 1025)},
		{name: "control in version", marker: "file", version: "v\n1"},
		{name: "empty truncated page", marker: "file", version: "next", empty: true},
		{name: "unchanged marker pair", marker: "file", version: "previous", cursor: versionCursor("test-bucket", "file", "file", "previous")},
		{name: "unchanged opaque marker pair", marker: "file[minio_cache:v2,return:]", version: "previous", cursor: versionCursor("test-bucket", "file", "file[minio_cache:v2,return:]", "previous")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := versionReader(func(context.Context, string, string, string, string, int) (storage.VersionPage, error) {
				rows := []storage.VersionRow{{Key: "file", ID: "v1"}}
				if tc.empty {
					rows = nil
				}
				return storage.VersionPage{Rows: rows, Truncated: true, KeyMarker: tc.marker, VersionMarker: tc.version}, nil
			})
			page, err := ListVersions(context.Background(), reader, "test-bucket", "file", tc.cursor)
			if err == nil || apperror.Public(err).Code != "VersionListingFailed" || len(page.Rows) != 0 || page.Next != "" {
				t.Fatal("unusable continuation accepted or partial rows published", page, err)
			}
		})
	}
}

func TestVersionPaginationKeepsOpaqueMarkersAndStopsAtNeighbor(t *testing.T) {
	key := "folder/a +%.txt"
	opaqueMarker := key + "[minio_cache:v2,return:]"
	reader := versionReader(func(_ context.Context, _, _ string, marker, version string, _ int) (storage.VersionPage, error) {
		if marker == "" {
			return storage.VersionPage{Rows: []storage.VersionRow{{Key: url.QueryEscape(key), ID: "null", Deleted: true}}, Truncated: true, KeyMarker: url.QueryEscape(opaqueMarker), VersionMarker: "opaque+/=%"}, nil
		}
		if marker != opaqueMarker || version != "opaque+/=%" {
			t.Fatal("opaque continuation changed", marker, version)
		}
		return storage.VersionPage{Rows: []storage.VersionRow{{Key: url.QueryEscape(key), ID: "older"}, {Key: url.QueryEscape(key + "-neighbor"), ID: "other"}}, Truncated: true, KeyMarker: url.QueryEscape(key + "-neighbor")}, nil
	})
	page, err := ListVersions(context.Background(), reader, "test-bucket", key, "")
	if err != nil || len(page.Rows) != 1 || !page.Rows[0].Deleted || page.Next == "" {
		t.Fatal(page, err)
	}
	page, err = ListVersions(context.Background(), reader, "test-bucket", key, page.Next)
	if err != nil || len(page.Rows) != 1 || page.Rows[0].ID != "older" || page.Next != "" {
		t.Fatal("neighbor must exhaust this object without requiring its version marker", page, err)
	}
	bad := versionCursor("test-bucket", key+"-neighbor", key+"-neighbor", "v1")
	if _, err := ListVersions(context.Background(), nil, "test-bucket", key, bad); err == nil || apperror.Public(err).Code != "InvalidCursor" {
		t.Fatal("foreign marker reached adapter", err)
	}
}

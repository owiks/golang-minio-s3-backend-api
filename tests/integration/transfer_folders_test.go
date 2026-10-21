package integration

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	folderssvc "minio-web/api/src/services/folders"
	"minio-web/api/src/services/objects/write"
	transferssvc "minio-web/api/src/services/transfers"
)

func TestTransferMapping(t *testing.T) {
	for _, tc := range []struct {
		Source, Destination, Sp, Dp string `json:"-"`
		Same, Want                  bool   `json:"-"`
	}{
		{"a/file", "b/file", "a/", "b/", true, true}, {"a/", "b/", "a/", "b/", true, true},
		{"a/file", "a/deep/a/file", "a/", "a/deep/a/", true, false},
		{"a/a/file", "a/file", "a/a/", "a/", true, false},
		{"a/file", "a/deep/a/file", "a/", "a/deep/a/", false, true},
		{"outside/file", "b/file", "a/", "b/", false, false},
		{"a/file", "b/other", "a/", "b/", false, false},
		{"a.txt", "b.txt", "", "", true, true}, {"a.txt", "b/", "", "", true, false}, {"a/../file", "b/file", "", "", true, false},
	} {
		if got := transferssvc.ValidTransferMapping(tc.Source, tc.Destination, tc.Sp, tc.Dp, tc.Same); got != tc.Want {
			t.Fatalf("%+v: %v", tc, got)
		}
	}
	if transferssvc.ValidTransferKey("/") || transferssvc.ValidTransferKey("") || transferssvc.ValidTransferKey("a/../") {
		t.Fatal("invalid transfer key accepted")
	}
}
func TestTransferFoldersIntegration(t *testing.T) {
	c, s3, b := fileFixture(t)
	ctx := context.Background()
	for _, key := range []string{"source/", "source/child.txt", "empty/"} {
		body := ""
		if !strings.HasSuffix(key, "/") {
			body = "data"
		}
		if _, err := s3.PutObject(ctx, b, key, strings.NewReader(body), int64(len(body)), minio.PutObjectOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	state := func(key string) objectwrite.FileState {
		var value objectwrite.FileState
		_ = json.Unmarshal(c.call("GET", "objects/transfer-prepare?"+url.Values{"bucket": {b}, "key": {key}}.Encode(), nil, 200), &value)
		return value
	}
	var manifest struct{ Items []folderssvc.FolderDeleteItem }
	_ = json.Unmarshal(c.call("GET", "objects/transfer-list?"+url.Values{"bucket": {b}, "prefix": {"source/"}}.Encode(), nil, 200), &manifest)
	if len(manifest.Items) != 2 {
		t.Fatal(manifest)
	}
	for _, parameter := range []string{"version", "versionId"} {
		for _, version := range []string{"", "old-version"} {
			var reply struct {
				Error apperror.Error `json:"error"`
			}
			q := url.Values{"bucket": {b}, "prefix": {"source/"}, parameter: {version}}
			if err := json.Unmarshal(c.call("GET", "objects/transfer-list?"+q.Encode(), nil, 400), &reply); err != nil || reply.Error.Code != "InvalidFolderDelete" {
				t.Fatal("folder version request accepted or error changed", parameter, version, reply, err)
			}
		}
	}
	if !state("source/").Exists || !state("source/child.txt").Exists || state("missing/").Exists {
		t.Fatal("preparation lost file/folder destination state")
	}
	move := func(key, dest, sp, dp string) map[string]any {
		var result map[string]any
		body := map[string]any{"sourceBucket": b, "sourceKey": key, "sourceRevision": state(key).Revision, "destinationBucket": b, "destinationKey": dest, "destinationRevision": state(dest).Revision, "sourcePrefix": sp, "destinationPrefix": dp, "move": true}
		_ = json.Unmarshal(c.call("POST", "objects/transfer", body, 200), &result)
		return result
	}
	// A parent marker is copied but retained until every child has moved.
	if result := move("source/", "dest/", "source/", "dest/"); result["moved"] != false || result["copied"] != true {
		t.Fatal(result)
	}
	if result := move("source/child.txt", "dest/child.txt", "source/", "dest/"); result["moved"] != true {
		t.Fatal(result)
	}
	if result := move("source/", "dest/", "source/", "dest/"); result["moved"] != true {
		t.Fatal(result)
	}
	if result := move("empty/", "empty-dest/", "empty/", "empty-dest/"); result["moved"] != true {
		t.Fatal(result)
	}
	if state("source/").Exists || state("source/child.txt").Exists || !state("dest/").Exists || !state("empty-dest/").Exists {
		t.Fatal("unexpected source or destination state")
	}
	c.call("POST", "objects/transfer", map[string]any{"sourceBucket": b, "sourceKey": "dest/child.txt", "sourceRevision": state("dest/child.txt").Revision, "destinationBucket": b, "destinationKey": "dest/inside/child.txt", "destinationRevision": "absent", "sourcePrefix": "dest/", "destinationPrefix": "dest/inside/", "move": true}, 400)
	if !state("dest/child.txt").Exists {
		t.Fatal("recursive move touched source")
	}
}

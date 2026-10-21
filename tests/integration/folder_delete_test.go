package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	folderscontroller "minio-web/api/src/http/api/routes/folders/controllers"
	folderssvc "minio-web/api/src/services/folders"
	"minio-web/api/src/services/securetoken"
)

func folderDeleteURL(bucket, prefix string, key *string) string {
	q := url.Values{"bucket": {bucket}, "prefix": {prefix}}
	if key != nil {
		q.Set("key", *key)
	}
	return "objects/folder-delete?" + q.Encode()
}
func folderSnapshot(t *testing.T, c *apiClient, b, prefix, key string) string {
	t.Helper()
	var data struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(c.call("GET", folderDeleteURL(b, prefix, &key), nil, 200), &data); err != nil || data.Revision == "" {
		t.Fatal(data, err)
	}
	return data.Revision
}
func deleteFolderItem(t *testing.T, c *apiClient, b, prefix, key string, want int) {
	t.Helper()
	rev := folderSnapshot(t, c, b, prefix, key)
	c.call("DELETE", folderDeleteURL(b, prefix, &key), map[string]string{"revision": rev, "confirmation": prefix}, want)
}
func TestFolderDeleteParams(t *testing.T) {
	for _, prefix := range []string{"", "without-slash", "a\x00/", strings.Repeat("a", 1025) + "/", "\xff/"} {
		r := httptest.NewRequest("GET", "/test?"+url.Values{"bucket": {"test-data"}, "prefix": {prefix}}.Encode(), nil)
		if _, _, err := folderssvc.FolderDeleteParams(r.URL.Query().Get("bucket"), r.URL.Query().Get("prefix")); err == nil {
			t.Fatalf("accepted %q", prefix)
		}
	}
	for _, prefix := range []string{"a/", "папка + # %/", "opaque//", "/"} {
		r := httptest.NewRequest("GET", "/test?"+url.Values{"bucket": {"test-data"}, "prefix": {prefix}}.Encode(), nil)
		if _, got, err := folderssvc.FolderDeleteParams(r.URL.Query().Get("bucket"), r.URL.Query().Get("prefix")); err != nil || got != prefix {
			t.Fatalf("changed opaque prefix %q: %q %v", prefix, got, err)
		}
	}
	for _, param := range []string{"version", "versionId"} {
		r := httptest.NewRequest("GET", "/test?bucket=test-data&prefix=a%2F&"+param+"=x", nil)
		if err := folderscontroller.HandleInspectFolderDeletion(nil, httptest.NewRecorder(), r, nil); err == nil {
			t.Fatal("accepted version deletion")
		}
	}
}
func TestFolderDeleteIntegration(t *testing.T) {
	c, s3, b := fileFixture(t)
	ctx := context.Background()
	prefix := "Папка + # %/"
	neighbor := "Папка + # %-backup/keep.txt"
	for key, body := range map[string]string{prefix: "", prefix + "a.txt": "abc", prefix + "empty.txt": "", prefix + "nested/": "", prefix + "nested/deep.txt": "defg", prefix + "empty/": "", neighbor: "keep"} {
		if _, err := s3.PutObject(ctx, b, key, strings.NewReader(body), int64(len(body)), minio.PutObjectOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	var manifest struct {
		Items          []folderssvc.FolderDeleteItem `json:"items"`
		Files, Markers int
		Size           int64
	}
	if err := json.Unmarshal(c.call("GET", folderDeleteURL(b, prefix, nil), nil, 200), &manifest); err != nil || manifest.Files != 3 || manifest.Markers != 3 || manifest.Size != 7 || len(manifest.Items) != 6 {
		t.Fatal(manifest, err)
	}
	key := prefix + "a.txt"
	rev := folderSnapshot(t, c, b, prefix, key)
	c.call("DELETE", folderDeleteURL(b, prefix, &key), map[string]string{"revision": rev, "confirmation": "wrong/"}, 400)
	c.call("DELETE", folderDeleteURL(b, prefix, &key), map[string]string{"confirmation": prefix}, 400)
	c.call("GET", folderDeleteURL(b, prefix, &neighbor), nil, 400)
	c.call("DELETE", folderDeleteURL(b, prefix, &neighbor), map[string]string{"revision": rev, "confirmation": prefix}, 400)
	c.call("DELETE", folderDeleteURL(b, prefix, &key)+"&versionId=null", map[string]string{"revision": rev, "confirmation": prefix}, 400)
	c.call("POST", folderDeleteURL(b, prefix, nil), map[string]string{}, 405)
	c.call("GET", folderDeleteURL(b, "", nil), nil, 400)
	if _, err := s3.PutObject(ctx, b, key, strings.NewReader("changed"), 7, minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	c.call("DELETE", folderDeleteURL(b, prefix, &key), map[string]string{"revision": rev, "confirmation": prefix}, 409)
	deleteFolderItem(t, c, b, prefix, prefix, 409) // Parent marker is not removed while children remain.
	for _, item := range manifest.Items {
		if !item.Marker {
			deleteFolderItem(t, c, b, prefix, item.Key, 200)
		}
	}
	deleteFolderItem(t, c, b, prefix, prefix+"nested/", 200)
	deleteFolderItem(t, c, b, prefix, prefix+"empty/", 200)
	deleteFolderItem(t, c, b, prefix, prefix, 200)
	if err := json.Unmarshal(c.call("GET", folderDeleteURL(b, prefix, nil), nil, 200), &manifest); err != nil || len(manifest.Items) != 0 {
		t.Fatal(manifest, err)
	}
	if _, err := s3.StatObject(ctx, b, neighbor, minio.StatObjectOptions{}); err != nil {
		t.Fatal("neighbor removed", err)
	}
}
func TestFolderDeletePaginationIntegration(t *testing.T) {
	c, s3, b := fileFixture(t)
	ctx := context.Background()
	for i := 0; i < 503; i++ {
		if _, err := s3.PutObject(ctx, b, fmt.Sprintf("pages/deep/%04d.txt", i), strings.NewReader("x"), 1, minio.PutObjectOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	var data struct {
		Items []folderssvc.FolderDeleteItem
		Files int
		Size  int64
	}
	if err := json.Unmarshal(c.call("GET", folderDeleteURL(b, "pages/", nil), nil, 200), &data); err != nil || data.Files != 503 || len(data.Items) != 503 || data.Size != 503 {
		t.Fatal(data.Files, len(data.Items), data.Size, err)
	}
}
func TestFolderDeletePermissionIntegration(t *testing.T) {
	c, s3, b := fileFixture(t)
	ctx := context.Background()
	prefix := "data/"
	for key, body := range map[string]string{prefix: "", prefix + "remove.txt": "remove", prefix + "keep.txt": "keep"} {
		if _, err := s3.PutObject(ctx, b, key, strings.NewReader(body), int64(len(body)), minio.PutObjectOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	name := "folder-del-" + strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:8]))
	c.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": name, "secretKey": "Folder-delete-fixture-123"}, 200)
	doc := json.RawMessage(fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:ListAllMyBuckets"],"Resource":["*"]},{"Effect":"Allow","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::%s"]},{"Effect":"Allow","Action":["s3:GetObject","s3:DeleteObject"],"Resource":["arn:aws:s3:::%s/data/*"]},{"Effect":"Deny","Action":["s3:DeleteObject"],"Resource":["arn:aws:s3:::%s/data/keep.txt"]}]}`, b, b, b))
	c.call("POST", "admin/policies", map[string]any{"acknowledgeNonAtomicCreate": true, "name": name, "document": doc}, 200)
	c.call("POST", "admin/bindings", map[string]string{"user": name, "policy": name}, 200)
	defer func() {
		c.call("POST", "admin/bindings", map[string]any{"user": name, "policy": name, "remove": true}, 200)
		c.call("DELETE", "admin/users/"+name, map[string]string{"confirmation": name}, 200)
		c.call("DELETE", "admin/policies/"+name, map[string]string{"confirmation": name}, 200)
	}()
	limited := loginTest(t, c.Origin, name, "Folder-delete-fixture-123")
	deleteFolderItem(t, limited, b, prefix, prefix+"remove.txt", 200)
	deleteFolderItem(t, limited, b, prefix, prefix+"keep.txt", 403)
	deleteFolderItem(t, limited, b, prefix, prefix, 409)
	if _, err := s3.StatObject(ctx, b, prefix+"keep.txt", minio.StatObjectOptions{}); err != nil {
		t.Fatal("denied file removed", err)
	}
	if _, err := s3.StatObject(ctx, b, prefix, minio.StatObjectOptions{}); err != nil {
		t.Fatal("parent marker removed", err)
	}
}
func TestFolderDeleteLockedVersionsIntegration(t *testing.T) {
	c, s3, b := fileFixture(t)
	ctx := context.Background()
	locked := b + "-locked"
	if err := s3.MakeBucket(ctx, locked, minio.MakeBucketOptions{ObjectLocking: true}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		for item := range s3.ListObjects(ctx, locked, minio.ListObjectsOptions{Recursive: true, WithVersions: true}) {
			if item.Err == nil {
				_ = s3.PutObjectLegalHold(ctx, locked, item.Key, minio.PutObjectLegalHoldOptions{VersionID: item.VersionID, Status: holdPointer(minio.LegalHoldDisabled)})
				_ = s3.RemoveObject(ctx, locked, item.Key, minio.RemoveObjectOptions{VersionID: item.VersionID, GovernanceBypass: true})
			}
		}
		_ = s3.RemoveBucket(ctx, locked)
	}()
	key := "protected/file.txt"
	original, err := s3.PutObject(ctx, locked, key, strings.NewReader("protected"), 9, minio.PutObjectOptions{Mode: minio.Governance, RetainUntilDate: time.Now().Add(time.Hour), LegalHold: minio.LegalHoldEnabled})
	if err != nil {
		t.Fatal(err)
	}
	deleteFolderItem(t, c, locked, "protected/", key, 200)
	if _, err = s3.StatObject(ctx, locked, key, minio.StatObjectOptions{}); minio.ToErrorResponse(err).Code != "NoSuchKey" {
		t.Fatal("current object still visible", err)
	}
	if _, err = s3.StatObject(ctx, locked, key, minio.StatObjectOptions{VersionID: original.VersionID}); err != nil {
		t.Fatal("protected version removed", err)
	}
}

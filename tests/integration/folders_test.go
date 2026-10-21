package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	folderssvc "minio-web/api/src/services/folders"
	objectssvc "minio-web/api/src/services/objects"
	"minio-web/api/src/services/securetoken"
)

func TestFolderKeyValidation(t *testing.T) {
	for _, tc := range []struct {
		Prefix, Name, Want string `json:"-"`
	}{
		{"", "отчёты + # %", "отчёты + # %/"},
		{"родитель/", " фото-2026 ", "родитель/фото-2026/"},
		{"opaque//", "child", "opaque//child/"},
		{"", strings.Repeat("я", 511) + "x", strings.Repeat("я", 511) + "x/"},
	} {
		key, err := folderssvc.FolderKey(tc.Prefix, tc.Name)
		if err != nil || key != tc.Want {
			t.Fatalf("%+v: %q %v", tc, key, err)
		}
	}
	for _, tc := range []struct {
		Prefix, Name string `json:"-"`
	}{
		{"", ""}, {"", "  "}, {"", "."}, {"", ".."}, {"", "a/b"}, {"", "a\\b"}, {"", "a\x00b"}, {"", "a\nb"},
		{"", "\xff"}, {"", strings.Repeat("я", 512)}, {"parent/", strings.Repeat("a", 1020)},
		{"bad", "child"}, {"a/../", "child"}, {"a/./", "child"}, {"a\\b/", "child"}, {"a\x00b/", "child"}, {"\xff/", "child"},
	} {
		if _, err := folderssvc.FolderKey(tc.Prefix, tc.Name); err == nil {
			t.Fatalf("accepted %+v", tc)
		}
	}
}
func TestCreateFolderIntegration(t *testing.T) {
	c, s3, b := fileFixture(t)
	ctx := context.Background()
	create := func(prefix, name string, want int) {
		t.Helper()
		c.call("POST", "objects/folders", map[string]string{"bucket": b, "prefix": prefix, "name": name}, want)
	}
	create("", "отчёты + # %", 201)
	create("отчёты + # %/", "Вложенная", 201)
	for _, key := range []string{"отчёты + # %/", "отчёты + # %/Вложенная/"} {
		object, err := s3.StatObject(ctx, b, key, minio.StatObjectOptions{})
		if err != nil || object.Size != 0 || object.ContentType != "application/x-directory" {
			t.Fatalf("folder marker: %+v %v", object, err)
		}
	}
	var page struct {
		Entries []objectssvc.Entry `json:"entries"`
	}
	var parent struct {
		Entries []objectssvc.Entry `json:"entries"`
	}
	if err := json.Unmarshal(c.call("GET", "objects?"+url.Values{"bucket": {b}}.Encode(), nil, 200), &parent); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range parent.Entries {
		if item.Key == "отчёты + # %/" && item.Kind == "folder" {
			found = true
			marker, err := s3.StatObject(ctx, b, item.Key, minio.StatObjectOptions{})
			if err != nil || item.Modified == nil || !item.Modified.Equal(marker.LastModified) {
				t.Fatalf("folder date does not match marker: %+v %v", item, err)
			}
		}
	}
	if !found {
		t.Fatalf("created folder missing from parent: %+v", parent.Entries)
	}
	raw := c.call("GET", "objects?"+url.Values{"bucket": {b}, "prefix": {"отчёты + # %/Вложенная/"}}.Encode(), nil, 200)
	if err := json.Unmarshal(raw, &page); err != nil || len(page.Entries) != 0 {
		t.Fatalf("marker leaked into contents: %s %v", raw, err)
	}
	create("", "отчёты + # %", 409)
	_, err := s3.PutObject(ctx, b, "implicit/deep/file.txt", strings.NewReader("keep"), 4, minio.PutObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	create("", "implicit", 409)
	// Omitted timestamps must not inherit fields from a previously decoded row.
	parent.Entries = nil
	if err := json.Unmarshal(c.call("GET", "objects?"+url.Values{"bucket": {b}}.Encode(), nil, 200), &parent); err != nil {
		t.Fatal(err)
	}
	for _, item := range parent.Entries {
		if item.Key == "implicit/" && item.Modified != nil {
			t.Fatal("nested file date used as folder creation", item)
		}
	}
	if _, err = s3.StatObject(ctx, b, "implicit/", minio.StatObjectOptions{}); minio.ToErrorResponse(err).Code != "NoSuchKey" {
		t.Fatal("unexpected marker", err)
	}
	_, err = s3.PutObject(ctx, b, "occupied/", strings.NewReader("keep"), 4, minio.PutObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	create("", "occupied", 409)
	obj, err := s3.StatObject(ctx, b, "occupied/", minio.StatObjectOptions{})
	if err != nil || obj.Size != 4 {
		t.Fatal(obj, err)
	}
	create("", "../escape", 400)
	create("missing-slash", "child", 400)
	c.call("GET", "objects/folders", nil, 405)
	c.call("DELETE", "objects/folders", nil, 405)
}
func TestCreateFolderPermissionsIntegration(t *testing.T) {
	c, s3, b := fileFixture(t)
	name := "folder-" + strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:10]))
	secret := "Folder-fixture-secret-123"
	c.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": name, "secretKey": secret}, 200)
	doc := json.RawMessage(fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:ListAllMyBuckets"],"Resource":["*"]},{"Effect":"Allow","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::%s"]},{"Effect":"Allow","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::%s/allowed/*"]}]}`, b, b))
	c.call("POST", "admin/policies", map[string]any{"acknowledgeNonAtomicCreate": true, "name": name, "document": doc}, 200)
	c.call("POST", "admin/bindings", map[string]string{"user": name, "policy": name}, 200)
	defer func() {
		c.call("POST", "admin/bindings", map[string]any{"user": name, "policy": name, "remove": true}, 200)
		c.call("DELETE", "admin/users/"+name, map[string]string{"confirmation": name}, 200)
		c.call("DELETE", "admin/policies/"+name, map[string]string{"confirmation": name}, 200)
	}()
	limited := loginTest(t, c.Origin, name, secret)
	limited.call("POST", "objects/folders", map[string]string{"bucket": b, "prefix": "allowed/", "name": "child"}, 201)
	var listing struct {
		Entries []objectssvc.Entry `json:"entries"`
	}
	if err := json.Unmarshal(limited.call("GET", "objects?"+url.Values{"bucket": {b}, "prefix": {"allowed/"}}.Encode(), nil, 200), &listing); err != nil || len(listing.Entries) != 1 || listing.Entries[0].Modified != nil {
		t.Fatalf("optional metadata must not require object read access: %+v %v", listing, err)
	}
	limited.call("POST", "objects/folders", map[string]string{"bucket": b, "prefix": "forbidden/", "name": "child"}, 403)
	if _, err := s3.StatObject(context.Background(), b, "forbidden/child/", minio.StatObjectOptions{}); minio.ToErrorResponse(err).Code != "NoSuchKey" {
		t.Fatal("unauthorized marker exists", err)
	}
	req, _ := http.NewRequest("POST", c.Origin+c.Base+"/objects/folders", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	response, err := c.Http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal(response.StatusCode)
	}
}

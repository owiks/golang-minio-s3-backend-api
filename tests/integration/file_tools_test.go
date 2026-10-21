package integration

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"minio-web/api/tests/testsession"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	settings "minio-web/api/src/config"
	apihttp "minio-web/api/src/http/api"
	uploadscontroller "minio-web/api/src/http/api/routes/uploads/controllers"
	"minio-web/api/src/services/apperror"
	archivessvc "minio-web/api/src/services/archives"
	"minio-web/api/src/services/remotesource"
	runtimesvc "minio-web/api/src/services/runtime"
	"minio-web/api/src/services/uploads/invite"
	"minio-web/api/src/services/uploads/source"
)

func TestRemoteURLSecurity(t *testing.T) {
	for _, raw := range []string{"http://example.org/file", "https://user:pass@example.org/a", "https://example.org:8080/a", "https://example.org/file#secret", "file:///etc/passwd"} {
		if _, err := source.ParseURL(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if _, err := source.ParseURL("https://example.org/file?download=1"); err != nil {
		t.Fatal(err)
	}
	client := remotesource.New()
	defer client.Close()
	if _, err := client.Open(context.Background(), "https://127.0.0.1/file"); err == nil {
		t.Fatal("loopback source permitted")
	}
	if _, err := client.Open(context.Background(), "https://169.254.169.254/latest"); err == nil {
		t.Fatal("metadata service permitted")
	}
}

type remoteTestSource func(context.Context, string) (source.File, error)

func (f remoteTestSource) Open(ctx context.Context, raw string) (source.File, error) {
	return f(ctx, raw)
}

func TestZIPEntrySafety(t *testing.T) {
	for _, name := range []string{"../secret.txt", "/absolute.txt", "a/../../b", "a\\b", "C:/x"} {
		if archivessvc.ZipEntryReason(&zip.File{FileHeader: zip.FileHeader{Name: name}}) == "" {
			t.Fatal(name)
		}
	}
	if reason := archivessvc.ZipEntryReason(&zip.File{FileHeader: zip.FileHeader{Name: "folder/Файл + %.txt"}}); reason != "" {
		t.Fatal(reason)
	}
}
func TestTextContentIntegration(t *testing.T) {
	c, client, bucket := fileFixture(t)
	ctx := context.Background()
	key := "config + %.json"
	original := "{\r\n  \"a\": 1\r\n}\r\n"
	_, err := client.PutObject(ctx, bucket, key, strings.NewReader(original), int64(len(original)), minio.PutObjectOptions{ContentType: "application/json", CacheControl: "private", UserMetadata: map[string]string{"custom": "yes", "x-amz-meta-content-type": "not-a-header"}, UserTags: map[string]string{"owner": "test"}})
	if err != nil {
		t.Fatal(err)
	}
	route := "objects/content?" + url.Values{"bucket": {bucket}, "key": {key}}.Encode()
	var doc struct {
		Text, Revision string
		Writable       bool
	}
	_ = json.Unmarshal(c.call("GET", route, nil, 200), &doc)
	if doc.Text != original || !doc.Writable {
		t.Fatal(doc)
	}
	c.call("PUT", route, map[string]any{"revision": doc.Revision, "text": "{bad"}, 400)
	c.call("PUT", route, map[string]any{"revision": doc.Revision, "text": "{\"a\":2}"}, 200)
	c.call("PUT", route, map[string]any{"revision": doc.Revision, "text": "{\"a\":3}"}, 409)
	info, err := client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil || info.ContentType != "application/json" || info.Metadata.Get("Cache-Control") != "private" || info.UserMetadata["Custom"] != "yes" && info.UserMetadata["custom"] != "yes" {
		t.Fatal(info, err)
	}
	tags, err := client.GetObjectTagging(ctx, bucket, key, minio.GetObjectTaggingOptions{})
	if err != nil || tags.ToMap()["owner"] != "test" {
		t.Fatal(tags, err)
	}
	object, _ := client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	data, e := io.ReadAll(object)
	object.Close()
	if e != nil || string(data) != "{\"a\":2}" {
		t.Fatal(string(data), e)
	}
}
func TestStoredZIPIntegration(t *testing.T) {
	c, client, bucket := fileFixture(t)
	var buffer bytes.Buffer
	z := zip.NewWriter(&buffer)
	f, _ := z.Create("folder/тест.txt")
	f.Write([]byte("zip content"))
	f, _ = z.Create("../escape.txt")
	f.Write([]byte("unsafe"))
	z.Close()
	_, err := client.PutObject(context.Background(), bucket, "archive.zip", bytes.NewReader(buffer.Bytes()), int64(buffer.Len()), minio.PutObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	route := "objects/zip?" + url.Values{"bucket": {bucket}, "key": {"archive.zip"}}.Encode()
	var listing struct {
		Revision string
		Items    []archivessvc.ZipItem
	}
	json.Unmarshal(c.call("GET", route, nil, 200), &listing)
	if len(listing.Items) != 2 || listing.Items[1].Reason == "" {
		t.Fatal(listing)
	}
	body := map[string]any{"revision": listing.Revision, "index": 0, "bucket": bucket, "key": "unpacked/folder/тест.txt", "destinationRevision": "absent"}
	c.call("POST", route, body, 200)
	c.call("POST", route, body, 409)
	body["index"] = 1
	body["key"] = "escape.txt"
	c.call("POST", route, body, 400)
	body["index"] = 0
	body["revision"] = "stale"
	c.call("POST", route, body, 409)
}
func TestReceiveInviteConstraintsIntegration(t *testing.T) {
	c, client, bucket := fileFixture(t)
	var g uploadinvite.View
	json.Unmarshal(c.call("POST", "objects/invites", map[string]any{"bucket": bucket, "prefix": "incoming/", "maxSize": 100, "maxFiles": 2, "expiresIn": 3600}, 201), &g)
	if g.Token == "" {
		t.Fatal("missing capability")
	}
	// Use the same test API listener with no session cookie or CSRF token.
	base := c.Origin
	send := func(name, content string, want int) {
		req, _ := http.NewRequest("PUT", base+"/api/receive?"+url.Values{"name": {name}}.Encode(), strings.NewReader(content))
		req.Header.Set("Authorization", "Bearer "+g.Token)
		req.Header.Set("Origin", c.Origin)
		req.Header.Set("Content-Type", "text/plain")
		req.Header.Set("X-Upload-Size", fmtInt(len(content)))
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%d != %d: %s", resp.StatusCode, want, data)
		}
	}
	send("../escape", "x", 400)
	send("large.txt", strings.Repeat("x", 101), 400)
	send("one.txt", "one", 200)
	send("one.txt", "overwrite", 409)
	send("two.txt", "two", 200)
	send("three.txt", "three", 409)
	info, e := client.StatObject(context.Background(), bucket, "incoming/one.txt", minio.StatObjectOptions{})
	if e != nil || info.Size != 3 {
		t.Fatal(info, e)
	}
	c.call("DELETE", "objects/invites?token="+g.Token, nil, 200)
	send("revoked.txt", "x", 404)
}
func fmtInt(n int) string { return strconv.Itoa(n) }
func TestInviteExpiryAndOrigin(t *testing.T) {
	app := runtimesvc.New(settings.Config{})
	defer app.Close()
	if err := app.Invites.Add(uploadinvite.New(uploadinvite.Config{Token: "expired", Expires: time.Now().Add(-time.Minute)})); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/receive", nil)
	r.Header.Set("Authorization", "Bearer expired")
	w := httptest.NewRecorder()
	apihttp.ServeHTTP(app, w, r)
	if w.Code != 410 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestRemoteImportCommitIntegration(t *testing.T) {
	_, s3, bucket := fileFixture(t)
	calls := 0
	client := remoteTestSource(func(context.Context, string) (source.File, error) {
		calls++
		return source.File{Size: 7, Body: io.NopCloser(strings.NewReader("content")), ContentType: "text/plain"}, nil
	})
	app := runtimesvc.New(settings.Config{})
	defer app.Close()
	run := func(rev string) string {
		raw, _ := json.Marshal(map[string]any{"url": "https://example.org/file", "revision": rev})
		req := httptest.NewRequest("POST", "/objects/import?"+url.Values{"bucket": {bucket}, "key": {"remote.txt"}}.Encode(), bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		err := uploadscontroller.HandleImportFromURL(app, w, req, testsession.Native(t), client)
		if err != nil {
			return apperror.Public(err).Code
		}
		return w.Body.String()
	}
	if result := run("absent"); !strings.Contains(result, `"result"`) {
		t.Fatal(result)
	}
	object, _ := s3.GetObject(context.Background(), bucket, "remote.txt", minio.GetObjectOptions{})
	data, err := io.ReadAll(object)
	object.Close()
	if err != nil || string(data) != "content" {
		t.Fatal(string(data), err)
	}
	if result := run("absent"); result != "DestinationChanged" || calls != 1 {
		t.Fatal(result, calls)
	}
}
func TestInviteOwnershipAndListIntegration(t *testing.T) {
	c, _, bucket := fileFixture(t)
	var g uploadinvite.View
	json.Unmarshal(c.call("POST", "objects/invites", map[string]any{"bucket": bucket, "prefix": "", "maxSize": 100, "maxFiles": 1, "expiresIn": 600}, 201), &g)
	var rows []map[string]any
	json.Unmarshal(c.call("GET", "objects/invites?bucket="+bucket, nil, 200), &rows)
	if len(rows) != 1 || rows[0]["token"] != g.Token {
		t.Fatal(rows)
	}
	c.call("DELETE", "objects/invites?token="+g.Token, nil, 200)
	json.Unmarshal(c.call("GET", "objects/invites?bucket="+bucket, nil, 200), &rows)
	if len(rows) != 0 {
		t.Fatal(rows)
	}
}
func TestZIPCRCFailureDoesNotCommitIntegration(t *testing.T) {
	c, s3, bucket := fileFixture(t)
	var buffer bytes.Buffer
	z := zip.NewWriter(&buffer)
	entry, _ := z.CreateHeader(&zip.FileHeader{Name: "file.txt", Method: zip.Store})
	entry.Write([]byte("test body"))
	z.Close()
	data := buffer.Bytes()
	at := bytes.Index(data, []byte("test body"))
	if at < 0 {
		t.Fatal("fixture")
	}
	data[at] = 'X'
	_, err := s3.PutObject(context.Background(), bucket, "corrupt.zip", bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	route := "objects/zip?" + url.Values{"bucket": {bucket}, "key": {"corrupt.zip"}}.Encode()
	var listing struct{ Revision string }
	json.Unmarshal(c.call("GET", route, nil, 200), &listing)
	c.call("POST", route, map[string]any{"revision": listing.Revision, "index": 0, "bucket": bucket, "key": "file.txt", "destinationRevision": "absent"}, 400)
	_, err = s3.StatObject(context.Background(), bucket, "file.txt", minio.StatObjectOptions{})
	if minio.ToErrorResponse(err).Code != "NoSuchKey" {
		t.Fatal("corrupt entry committed", err)
	}
}

func TestRemoteSpecialNetworksAreNotPublic(t *testing.T) {
	for _, raw := range []string{"0.1.2.3", "240.1.2.3", "64:ff9b::a9fe:a9fe", "64:ff9b:1::a9fe:a9fe", "2002:7f00:1::1", "2001::1", "::ffff:127.0.0.1"} {
		if remotesource.PublicIP(net.ParseIP(raw)) {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "2606:4700::1111"} {
		if !remotesource.PublicIP(net.ParseIP(raw)) {
			t.Fatal(raw)
		}
	}
}

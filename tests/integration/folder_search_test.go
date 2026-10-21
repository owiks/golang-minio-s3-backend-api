package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	settings "minio-web/api/src/config"
	apihttp "minio-web/api/src/http/api"
	"minio-web/api/src/http/presentation"
	folderssvc "minio-web/api/src/services/folders"
	foldershare "minio-web/api/src/services/folders/share"
	objectssvc "minio-web/api/src/services/objects"
	runtimesvc "minio-web/api/src/services/runtime"
	searchsvc "minio-web/api/src/services/search"
)

func publicRead(t *testing.T, base, token string, params url.Values, want int) []byte {
	t.Helper()
	req, _ := http.NewRequest("GET", base+"/api/shared-folder?"+params.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		t.Fatalf("%s got %d want %d: %s", params.Encode(), resp.StatusCode, want, data)
	}
	return data
}
func TestFolderShareScopeAndDownloadIntegration(t *testing.T) {
	c, s3, bucket := fileFixture(t)
	ctx := context.Background()
	for key, value := range map[string]string{"shared/readme.txt": "hello", "shared/inner/Файл + # %.txt": "nested", "shared/%2e%2e/opaque.txt": "opaque", "private.txt": "private", "shared-other/leak.txt": "private", "shared/movie.mp4": "0123456789"} {
		_, err := s3.PutObject(ctx, bucket, key, strings.NewReader(value), int64(len(value)), minio.PutObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}
	var grant struct{ Token string }
	data := c.call("POST", "objects/folder-shares", map[string]any{"bucket": bucket, "prefix": "shared/", "expiresIn": 3600}, 201)
	json.Unmarshal(data, &grant)
	if strings.Contains(string(data), "secretKey") || strings.Contains(string(data), "minio-web-test-secret") || grant.Token == "" {
		t.Fatal(string(data))
	}
	publicRead(t, c.Origin, "", url.Values{"mode": {"info"}}, 404)
	publicRead(t, c.Origin, grant.Token, url.Values{"mode": {"list"}, "prefix": {""}}, 403)
	publicRead(t, c.Origin, grant.Token, url.Values{"mode": {"list"}, "prefix": {"shared-other/"}}, 403)
	var page struct{ Entries []objectssvc.Entry }
	json.Unmarshal(publicRead(t, c.Origin, grant.Token, url.Values{"mode": {"list"}, "prefix": {"shared/"}}, 200), &page)
	for _, row := range page.Entries {
		if !strings.HasPrefix(row.Key, "shared/") {
			t.Fatal(row)
		}
	}
	if len(page.Entries) < 3 {
		t.Fatal(page)
	}
	publicRead(t, c.Origin, grant.Token, url.Values{"mode": {"preview"}, "key": {"private.txt"}}, 403)
	if data := publicRead(t, c.Origin, grant.Token, url.Values{"mode": {"preview"}, "key": {"shared/%2e%2e/opaque.txt"}}, 200); string(data) != "opaque" {
		t.Fatal(string(data))
	}
	var ticket struct{ URL string }
	json.Unmarshal(publicRead(t, c.Origin, grant.Token, url.Values{"mode": {"ticket"}, "kind": {"download"}, "key": {"shared/inner/Файл + # %.txt"}}, 200), &ticket)
	response, e := http.Get(c.Origin + ticket.URL)
	if e != nil {
		t.Fatal(e)
	}
	payload, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || string(payload) != "nested" || !strings.Contains(response.Header.Get("Content-Disposition"), "attachment") {
		t.Fatal(response.StatusCode, string(payload), response.Header)
	}
	response, e = http.Get(c.Origin + ticket.URL)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 410 {
		t.Fatal("ticket reused", response.StatusCode)
	}
	json.Unmarshal(publicRead(t, c.Origin, grant.Token, url.Values{"mode": {"ticket"}, "kind": {"media"}, "key": {"shared/movie.mp4"}}, 200), &ticket)
	req, _ := http.NewRequest("GET", c.Origin+ticket.URL, nil)
	req.Header.Set("Range", "bytes=3-6")
	response, e = http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	payload, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 206 || string(payload) != "3456" || response.Header.Get("Content-Range") != "bytes 3-6/10" {
		t.Fatal(response.StatusCode, string(payload), response.Header)
	}
	c.call("DELETE", "objects/folder-shares?token="+grant.Token, nil, 200)
	publicRead(t, c.Origin, grant.Token, url.Values{"mode": {"info"}}, 404)
	response, e = http.Get(c.Origin + ticket.URL)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 410 {
		t.Fatal("revoked ticket", response.StatusCode)
	}
}
func TestFolderShareExpiryAndReadOnly(t *testing.T) {
	app := runtimesvc.New(settings.Config{})
	defer app.Close()
	if err := app.Shares.Add(foldershare.New(foldershare.Config{Token: "expired", Expires: time.Now().Add(-time.Second)})); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/shared-folder", nil)
	r.Header.Set("Authorization", "Bearer expired")
	w := httptest.NewRecorder()
	apihttp.ServeHTTP(app, w, r)
	if w.Code != 410 {
		t.Fatal(w.Code)
	}
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		w = httptest.NewRecorder()
		apihttp.ServeHTTP(app, w, httptest.NewRequest(method, "/api/shared-folder", nil))
		if w.Code != 405 {
			t.Fatal(method, w.Code)
		}
	}
}
func TestSharedRanges(t *testing.T) {
	for _, raw := range []string{"bytes=10-", "bytes=3-1", "bytes=0-1,3-4", "items=0-1", "bytes=-0"} {
		if _, _, e := folderssvc.ParseShareRange(raw, 10); e == nil {
			t.Fatal(raw)
		}
	}
	for _, sample := range []struct {
		Raw        string `json:"-"`
		Start, End int64  `json:"-"`
	}{{"bytes=2-", 2, 9}, {"bytes=-3", 7, 9}, {"bytes=0-99", 0, 9}} {
		start, end, err := folderssvc.ParseShareRange(sample.Raw, 10)
		if err != nil || start != sample.Start || end != sample.End {
			t.Fatal(sample, start, end, err)
		}
	}
}
func TestContentHitsUnicodeAndBounds(t *testing.T) {
	hits, truncated := searchsvc.FindTextHits("\ufefffirst\r\n🙂 ПРИВЕТ мир\r\nlast", "привет", false)
	if truncated || len(hits) != 1 || hits[0].Line != 2 || hits[0].Column != 3 || hits[0].Match != "ПРИВЕТ" {
		t.Fatal(hits, truncated)
	}
	if hits, _ := searchsvc.FindTextHits("ПРИВЕТ", "привет", true); len(hits) != 0 {
		t.Fatal(hits)
	}
	hits, truncated = searchsvc.FindTextHits(strings.Repeat("match\n", 21), "match", true)
	if len(hits) != 20 || !truncated {
		t.Fatal(len(hits), truncated)
	}
	hits, _ = searchsvc.FindTextHits(strings.Repeat("界", 500)+"needle"+strings.Repeat("界", 500), "needle", true)
	if len([]rune(hits[0].Before)) > 56 || len([]rune(hits[0].After)) > 91 {
		t.Fatal(hits)
	}
}
func TestContentSearchNativePaginationAndSkipsIntegration(t *testing.T) {
	c, s3, bucket := fileFixture(t)
	ctx := context.Background()
	for key, text := range map[string]string{"configs/a.json": "{\"domain\":\"old-domain.com\"}", "configs/b.yaml": "name: test\nsite: OLD-DOMAIN.COM\n", "configs/c.txt": "new-domain.com", "configs/d.txt": "old-domain.com\x00binary", "configs/e.bin": "old-domain.com", "configs/f.txt": strings.Repeat("x", 1025), "outside/a.txt": "old-domain.com"} {
		_, e := s3.PutObject(ctx, bucket, key, strings.NewReader(text), int64(len(text)), minio.PutObjectOptions{})
		if e != nil {
			t.Fatal(e)
		}
	}
	body := map[string]any{"bucket": bucket, "prefix": "configs/", "query": "old-domain.com", "extensions": " .JSON, yaml, .TXT, .json ", "caseSensitive": false, "maxSize": 1024, "pageSize": 2, "token": ""}
	matched, scanned, skipped, read, issues := 0, 0, 0, 0, 0
	for i := 0; i < 10; i++ {
		var result struct {
			Matches                []searchsvc.ContentMatch
			Issues                 []presentation.ContentIssue
			Scanned, Skipped, Read int
			NextToken              string
		}
		json.Unmarshal(c.call("POST", "objects/content-search", body, 200), &result)
		matched += len(result.Matches)
		scanned += result.Scanned
		skipped += result.Skipped
		read += result.Read
		issues += len(result.Issues)
		for _, row := range result.Matches {
			if strings.Contains(row.ETag, "\"") {
				t.Fatal("ETag must match StatObject representation", row.ETag)
			}
			if !strings.HasPrefix(row.Key, "configs/") {
				t.Fatal(row)
			}
		}
		if result.NextToken == "" {
			break
		}
		body["token"] = result.NextToken
	}
	if matched != 2 || scanned != 6 || skipped != 2 || read != 3 || issues != 1 {
		t.Fatal(matched, scanned, skipped, read, issues)
	}
	body["extensions"] = "exe"
	c.call("POST", "objects/content-search", body, 400)
	body["extensions"] = "txt"
	body["query"] = ""
	c.call("POST", "objects/content-search", body, 400)
}
func TestFolderAndContentLeastPrivilegeIntegration(t *testing.T) {
	root, s3, bucket := fileFixture(t)
	ctx := context.Background()
	for key, text := range map[string]string{"shared/allowed.txt": "needle allowed", "shared/denied.txt": "needle secret", "outside.txt": "needle outside"} {
		if _, e := s3.PutObject(ctx, bucket, key, strings.NewReader(text), int64(len(text)), minio.PutObjectOptions{}); e != nil {
			t.Fatal(e)
		}
	}
	doc := map[string]any{"Version": "2012-10-17", "Statement": []any{map[string]any{"Effect": "Allow", "Action": []string{"s3:ListBucket"}, "Resource": []string{"arn:aws:s3:::" + bucket}}, map[string]any{"Effect": "Allow", "Action": []string{"s3:GetObject"}, "Resource": []string{"arn:aws:s3:::" + bucket + "/shared/*"}}, map[string]any{"Effect": "Deny", "Action": []string{"s3:GetObject"}, "Resource": []string{"arn:aws:s3:::" + bucket + "/shared/denied.txt"}}}}
	var pair struct{ AccessKey, SecretKey string }
	json.Unmarshal(root.call("POST", "admin/access-keys", map[string]any{"name": "folder-limited", "policy": doc}, 200), &pair)
	if pair.AccessKey == "" {
		t.Fatal("missing key")
	}
	defer root.call("DELETE", "admin/access-keys/"+pair.AccessKey, map[string]any{"confirmation": pair.AccessKey}, 200)
	reader := loginTest(t, root.Origin, pair.AccessKey, pair.SecretKey)
	var grant struct{ Token string }
	json.Unmarshal(reader.call("POST", "objects/folder-shares", map[string]any{"bucket": bucket, "prefix": "shared/", "expiresIn": 600}, 201), &grant)
	publicRead(t, root.Origin, grant.Token, url.Values{"mode": {"preview"}, "key": {"shared/allowed.txt"}}, 200)
	publicRead(t, root.Origin, grant.Token, url.Values{"mode": {"preview"}, "key": {"shared/denied.txt"}}, 403)
	reader.call("POST", "objects/folder-shares", map[string]any{"bucket": bucket, "prefix": "", "expiresIn": 600}, 403)
	var result struct {
		Matches []searchsvc.ContentMatch
		Issues  []presentation.ContentIssue
	}
	json.Unmarshal(reader.call("POST", "objects/content-search", map[string]any{"bucket": bucket, "prefix": "shared/", "query": "needle", "extensions": "txt", "maxSize": 1024, "pageSize": 20}, 200), &result)
	if len(result.Matches) != 1 || result.Matches[0].Key != "shared/allowed.txt" || len(result.Issues) != 1 || result.Issues[0].Key != "shared/denied.txt" {
		t.Fatal(result)
	}
	root.call("DELETE", "objects/folder-shares?token="+grant.Token, nil, 404)
}
func TestFolderRevokeCancelsActiveRequests(t *testing.T) {
	app := runtimesvc.New(settings.Config{})
	defer app.Close()
	g := foldershare.New(foldershare.Config{Expires: time.Now().Add(time.Hour)})
	r, release, err := g.BeginRead(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	g.Stop()
	if r.Err() == nil {
		t.Fatal("active request not cancelled")
	}
	release()
	if app.ActiveTransfers() != 0 {
		t.Fatal("slot leaked")
	}
	if _, _, err = g.BeginRead(context.Background(), app); err == nil {
		t.Fatal("revoked link reused")
	}
}

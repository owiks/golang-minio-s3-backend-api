package integration

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/tags"

	settings "minio-web/api/src/config"
	apihttp "minio-web/api/src/http/api"
	"minio-web/api/src/http/presentation"
	httpvalidation "minio-web/api/src/http/validation"
	archiveplan "minio-web/api/src/services/archives/plan"
	objectssvc "minio-web/api/src/services/objects"
	objectwrite "minio-web/api/src/services/objects/write"
	runtimesvc "minio-web/api/src/services/runtime"
	"minio-web/api/src/services/securetoken"
	uploadssvc "minio-web/api/src/services/uploads"
	validationsvc "minio-web/api/src/services/validation"
)

func fileFixture(t *testing.T, configure ...func(*settings.Config)) (*apiClient, *minio.Client, string) {
	t.Helper()
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("isolated fixture")
	}
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
	for _, apply := range configure {
		apply(&cfg)
	}
	app := runtimesvc.New(cfg)
	srv := httptest.NewServer(apihttp.NewHandler(app))
	client, err := minio.New("127.0.0.1:19000", &minio.Options{Creds: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", ""), Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	bucket := "files-" + strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:10]))
	if err := client.MakeBucket(context.Background(), bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for o := range client.ListObjects(context.Background(), bucket, minio.ListObjectsOptions{Recursive: true, WithVersions: true}) {
			if o.Err == nil {
				_ = client.RemoveObject(context.Background(), bucket, o.Key, minio.RemoveObjectOptions{VersionID: o.VersionID})
			}
		}
		_ = client.RemoveBucket(context.Background(), bucket)
		srv.Close()
		app.Close()
	})
	return loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret"), client, bucket
}
func uploadTest(t *testing.T, c *apiClient, bucket, key, rev string, data []byte, want int) {
	t.Helper()
	q := url.Values{"bucket": {bucket}, "key": {key}}
	r, err := http.NewRequest("PUT", c.Origin+c.Base+"/objects/upload?"+q.Encode(), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Origin", c.Origin)
	r.Header.Set("X-CSRF-Token", c.CSRF)
	r.Header.Set("X-Upload-Size", fmtSize(len(data)))
	r.Header.Set("X-Object-Revision", rev)
	r.Header.Set("Content-Type", "text/plain")
	res, err := c.Http.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != want {
		t.Fatalf("upload %d want %d: %s", res.StatusCode, want, body)
	}
}
func fmtSize(size int) string { return strconv.Itoa(size) }
func TestUploadIntegration(t *testing.T) {
	c, s3, b := fileFixture(t)
	key := "папка/Привет + # %.txt"
	q := url.Values{"bucket": {b}, "key": {key}}
	var state objectwrite.FileState
	_ = json.Unmarshal(c.call("GET", "objects/prepare?"+q.Encode(), nil, 200), &state)
	if state.Exists || state.Revision != "absent" {
		t.Fatal(state)
	}
	uploadTest(t, c, b, key, state.Revision, []byte("before"), 200)
	uploadTest(t, c, b, key, "absent", []byte("must not overwrite"), 409)
	_ = json.Unmarshal(c.call("GET", "objects/prepare?"+q.Encode(), nil, 200), &state)
	_, err := s3.PutObject(context.Background(), b, key, strings.NewReader("external"), 8, minio.PutObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	uploadTest(t, c, b, key, state.Revision, []byte("stale"), 409)
	_ = json.Unmarshal(c.call("GET", "objects/prepare?"+q.Encode(), nil, 200), &state)
	uploadTest(t, c, b, key, state.Revision, []byte("after"), 200)
	uploadTest(t, c, b, "empty.txt", "absent", nil, 200)
	uploadTest(t, c, b, "multipart.bin", "absent", bytes.Repeat([]byte("x"), 17*1024*1024), 200)
	for _, k := range []string{"empty.txt", "multipart.bin", key} {
		o, e := s3.StatObject(context.Background(), b, k, minio.StatObjectOptions{})
		if e != nil {
			t.Fatal(e)
		}
		if k == "empty.txt" && o.Size != 0 {
			t.Fatal(o)
		}
	}
	// Conditional writes protect the destination even when it changes after HEAD.
	opts, _ := objectwrite.WriteOptions(objectwrite.FileState{Revision: "absent"}, "absent", "text/plain")
	_, err = s3.PutObject(context.Background(), b, key, strings.NewReader("bad"), 3, opts)
	if minio.ToErrorResponse(err).Code != "PreconditionFailed" {
		t.Fatalf("conditional create: %v", err)
	}
}
func TestFileKeyValidation(t *testing.T) {
	for _, k := range []string{"", "a/../b", "a/./b", "a\\b", "a/", "a\x00b"} {
		if validationsvc.ValidFileKey(k) {
			t.Fatal(k)
		}
	}
	if !validationsvc.ValidFileKey("фото/Привет + # %.jpg") {
		t.Fatal("unicode")
	}
}

func TestTransferIntegration(t *testing.T) {
	c, s3, b := fileFixture(t)
	ctx := context.Background()
	source := "source.txt"
	dest := "копия + # %.txt"
	_, err := s3.PutObject(ctx, b, source, strings.NewReader("data"), 4, minio.PutObjectOptions{ContentType: "text/plain", UserMetadata: map[string]string{"project": "test"}})
	if err != nil {
		t.Fatal(err)
	}
	tagset, _ := tags.NewTags(map[string]string{"team": "qa"}, true)
	if err = s3.PutObjectTagging(ctx, b, source, tagset, minio.PutObjectTaggingOptions{}); err != nil {
		t.Fatal(err)
	}
	state := func(key string) objectwrite.FileState {
		var state objectwrite.FileState
		_ = json.Unmarshal(c.call("GET", "objects/prepare?"+url.Values{"bucket": {b}, "key": {key}}.Encode(), nil, 200), &state)
		return state
	}
	body := map[string]any{"sourceBucket": b, "sourceKey": source, "sourceRevision": state(source).Revision, "destinationBucket": b, "destinationKey": dest, "destinationRevision": "absent", "move": false}
	c.call("POST", "objects/transfer", body, 200)
	copy, err := s3.StatObject(ctx, b, dest, minio.StatObjectOptions{})
	if err != nil || copy.Size != 4 || copy.UserMetadata["Project"] != "test" {
		t.Fatal(copy, err)
	}
	gotTags, err := s3.GetObjectTagging(ctx, b, dest, minio.GetObjectTaggingOptions{})
	if err != nil || gotTags.ToMap()["team"] != "qa" {
		t.Fatal(gotTags, err)
	}
	c.call("POST", "objects/transfer", body, 409) // destination no longer absent
	body["destinationKey"] = "renamed.txt"
	body["move"] = true
	var result struct {
		Copied, Moved bool
		Code          string
	}
	_ = json.Unmarshal(c.call("POST", "objects/transfer", body, 200), &result)
	if !result.Copied || !result.Moved || result.Code != "" {
		t.Fatal(result)
	}
	if _, err = s3.StatObject(ctx, b, source, minio.StatObjectOptions{}); minio.ToErrorResponse(err).Code != "NoSuchKey" {
		t.Fatal("source remains after successful move", err)
	}
	body["sourceKey"] = "renamed.txt"
	body["sourceRevision"] = "stale"
	body["destinationKey"] = "bad.txt"
	c.call("POST", "objects/transfer", body, 409)
}

func TestRecursiveSearchIntegration(t *testing.T) {
	c, s3, b := fileFixture(t)
	ctx := context.Background()
	for i := 0; i < 505; i++ {
		key := "nested/" + fmtSize(i) + ".txt"
		if _, err := s3.PutObject(ctx, b, key, strings.NewReader("data"), 4, minio.PutObjectOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	query := url.Values{"bucket": {b}, "q": {"NESTED/"}, "extensions": {" .TXT, txt "}, "minSize": {"4"}, "maxSize": {"4"}}
	var page struct {
		Entries   []objectssvc.Entry `json:"entries"`
		Scanned   int                `json:"scanned"`
		NextToken string             `json:"nextToken"`
	}
	_ = json.Unmarshal(c.call("GET", "objects/search?"+query.Encode(), nil, 200), &page)
	if page.Scanned != 500 || len(page.Entries) != 500 || page.NextToken == "" {
		t.Fatal(len(page.Entries), page.Scanned, page.NextToken)
	}
	query.Set("token", page.NextToken)
	_ = json.Unmarshal(c.call("GET", "objects/search?"+query.Encode(), nil, 200), &page)
	if page.Scanned != 5 || len(page.Entries) != 5 || page.NextToken != "" {
		t.Fatal(len(page.Entries), page.Scanned, page.NextToken)
	}
	query.Del("token")
	query.Set("extensions", "mp4")
	_ = json.Unmarshal(c.call("GET", "objects/search?"+query.Encode(), nil, 200), &page)
	if len(page.Entries) != 0 || page.Scanned != 500 || page.NextToken == "" {
		t.Fatal("empty filtered page must keep cursor", page)
	}
	query.Set("minSize", "8")
	c.call("GET", "objects/search?"+query.Encode(), nil, 400)
}

func TestArchiveIntegration(t *testing.T) {
	c, s3, b := fileFixture(t)
	ctx := context.Background()
	for _, key := range []string{"tree/a.txt", "tree/deep/б + # %.txt"} {
		if _, err := s3.PutObject(ctx, b, key, strings.NewReader("archive"), 7, minio.PutObjectOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	var plan struct {
		Ticket string `json:"ticket"`
		Count  int    `json:"count"`
	}
	_ = json.Unmarshal(c.call("POST", "objects/archive", map[string]any{"bucket": b, "prefix": "tree/"}, 200), &plan)
	if plan.Count != 2 || plan.Ticket == "" {
		t.Fatal(plan)
	}
	_ = s3.RemoveObject(ctx, b, "tree/a.txt", minio.RemoveObjectOptions{})
	data := c.call("GET", "objects/archive/"+plan.Ticket, nil, 200)
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) != 2 {
		t.Fatal("one file plus manifest expected", len(archive.File))
	}
	var manifest struct {
		Files []presentation.ArchiveItem `json:"files"`
	}
	for _, file := range archive.File {
		reader, e := file.Open()
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := io.ReadAll(reader)
		reader.Close()
		if strings.HasPrefix(file.Name, ".minio-web-results-") {
			if json.Unmarshal(raw, &manifest) != nil {
				t.Fatal("manifest")
			}
		} else if string(raw) != "archive" {
			t.Fatal(file.Name, string(raw))
		}
	}
	if len(manifest.Files) != 2 || manifest.Files[0].State != "error" || manifest.Files[1].State != "done" {
		t.Fatal(manifest)
	}
	c.call("GET", "objects/archive/"+plan.Ticket, nil, 409)
	var status struct {
		Finished bool                       `json:"finished"`
		Items    []presentation.ArchiveItem `json:"items"`
	}
	_ = json.Unmarshal(c.call("GET", "objects/archive-status/"+plan.Ticket, nil, 200), &status)
	if !status.Finished || len(status.Items) != 2 {
		t.Fatal(status)
	}
	other := loginTest(t, c.Origin, "minio-web-test", "minio-web-test-secret")
	other.call("GET", "objects/archive-status/"+plan.Ticket, nil, 404)
	c.call("POST", "objects/archive", map[string]any{"bucket": b, "keys": []string{"../unsafe.txt"}}, 400)
	longKey := strings.Repeat("a", 100) + ".txt"
	if _, err = s3.PutObject(ctx, b, longKey, strings.NewReader("x"), 1, minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 1000)
	for i := range keys {
		keys[i] = longKey
	}
	// A full selection with long keys can exceed the 64 KiB form limit.
	_ = json.Unmarshal(c.call("POST", "objects/archive", map[string]any{"bucket": b, "keys": keys}, 200), &plan)
	if plan.Count != 1 {
		t.Fatal("duplicate keys must not duplicate ZIP entries", plan)
	}
}
func TestSafeZipPaths(t *testing.T) {
	for _, key := range []string{"../a", "/absolute", "a//b", "C:evil", "a\\b"} {
		if archiveplan.SafeArchivePath(key) {
			t.Fatal(key)
		}
	}
	if !archiveplan.SafeArchivePath("папка/файл + # %.txt") {
		t.Fatal("unicode")
	}
}

func TestIncompleteUploadIntegration(t *testing.T) {
	c, s3, b := fileFixture(t)
	ctx := context.Background()
	core := minio.Core{Client: s3}
	key := "archives/large.zip"
	_, err := s3.PutObject(ctx, b, key, strings.NewReader("ready"), 5, minio.PutObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	id, err := core.NewMultipartUpload(ctx, b, key, minio.PutObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer core.AbortMultipartUpload(ctx, b, key, id)
	if _, err = core.PutObjectPart(ctx, b, key, id, 1, strings.NewReader("partial"), 7, minio.PutObjectPartOptions{}); err != nil {
		t.Fatal(err)
	}
	var listing struct {
		Items []struct {
			Key   string                 `json:"key"`
			ID    string                 `json:"uploadId"`
			Parts uploadssvc.UploadParts `json:"parts"`
		} `json:"items"`
	}
	_ = json.Unmarshal(c.call("GET", "admin/incomplete-uploads?bucket="+b, nil, 200), &listing)
	if len(listing.Items) != 1 || listing.Items[0].ID != id || listing.Items[0].Parts.Size != 7 || listing.Items[0].Parts.Count != 1 {
		t.Fatal(listing)
	}
	var summary struct {
		Items []struct {
			ID    string                  `json:"uploadId"`
			Parts *uploadssvc.UploadParts `json:"parts"`
		} `json:"items"`
	}
	_ = json.Unmarshal(c.call("GET", "admin/incomplete-uploads?bucket="+b+"&prefix=archives%2F&summary=1", nil, 200), &summary)
	if len(summary.Items) != 1 || summary.Items[0].ID != id || summary.Items[0].Parts != nil {
		t.Fatal("fast summary", summary)
	}
	var details struct {
		State string                 `json:"state"`
		Parts uploadssvc.UploadParts `json:"parts"`
	}
	_ = json.Unmarshal(c.call("GET", "admin/incomplete-uploads/parts?"+url.Values{"bucket": {b}, "key": {key}, "uploadId": {id}}.Encode(), nil, 200), &details)
	if details.State != "present" || len(details.Parts.Items) != 1 || details.Parts.LastModified.IsZero() {
		t.Fatal("parts detail", details)
	}
	body := map[string]any{"bucket": b, "key": key, "uploadId": id, "revision": listing.Items[0].Parts.Revision, "confirmation": "ОТМЕНИТЬ"}
	if _, err = core.PutObjectPart(ctx, b, key, id, 2, strings.NewReader("more"), 4, minio.PutObjectPartOptions{}); err != nil {
		t.Fatal(err)
	}
	c.call("POST", "admin/incomplete-uploads/abort", body, 409)
	_ = json.Unmarshal(c.call("GET", "admin/incomplete-uploads?bucket="+b, nil, 200), &listing)
	body["revision"] = listing.Items[0].Parts.Revision
	c.call("POST", "admin/incomplete-uploads/abort", body, 200)
	if _, err = core.ListObjectParts(ctx, b, key, id, 0, 1000); minio.ToErrorResponse(err).Code != "NoSuchUpload" {
		t.Fatal(err)
	}
	if info, err := s3.StatObject(ctx, b, key, minio.StatObjectOptions{}); err != nil || info.Size != 5 {
		t.Fatal("ready object changed", info, err)
	}
}

func TestTransferDeleteDeniedKeepsBothCopies(t *testing.T) {
	c, s3, b := fileFixture(t)
	ctx := context.Background()
	key := "keep-source.txt"
	if _, err := s3.PutObject(ctx, b, key, strings.NewReader("original"), 8, minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	name := "copy-" + strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:10]))
	secret := "Transfer-no-delete-fixture-123"
	c.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": name, "secretKey": secret}, 200)
	policy := json.RawMessage(fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:ListAllMyBuckets"],"Resource":["*"]},{"Effect":"Allow","Action":["s3:ListBucket","s3:GetObject","s3:GetObjectTagging","s3:PutObject","s3:PutObjectTagging"],"Resource":["arn:aws:s3:::%s","arn:aws:s3:::%s/*"]}]}`, b, b))
	c.call("POST", "admin/policies", map[string]any{"acknowledgeNonAtomicCreate": true, "name": name, "document": policy}, 200)
	c.call("POST", "admin/bindings", map[string]any{"user": name, "policy": name}, 200)
	defer func() {
		c.call("POST", "admin/bindings", map[string]any{"user": name, "policy": name, "remove": true}, 200)
		c.call("DELETE", "admin/users/"+name, map[string]string{"confirmation": name}, 200)
		c.call("DELETE", "admin/policies/"+name, map[string]string{"confirmation": name}, 200)
	}()
	operator := loginTest(t, c.Origin, name, secret)
	var state objectwrite.FileState
	_ = json.Unmarshal(operator.call("GET", "objects/prepare?"+url.Values{"bucket": {b}, "key": {key}}.Encode(), nil, 200), &state)
	var result struct {
		Copied, Moved bool
		Message, Code string
	}
	_ = json.Unmarshal(operator.call("POST", "objects/transfer", map[string]any{"sourceBucket": b, "sourceKey": key, "sourceRevision": state.Revision, "destinationBucket": b, "destinationKey": "copy.txt", "destinationRevision": "absent", "move": true}, 200), &result)
	if !result.Copied || result.Moved || result.Code != "ObjectDeleteDenied" {
		t.Fatal(result)
	}
	for _, key := range []string{key, "copy.txt"} {
		if info, err := s3.StatObject(ctx, b, key, minio.StatObjectOptions{}); err != nil || info.Size != 8 {
			t.Fatal(key, info, err)
		}
	}
}

func TestUploadReadOnlyDenied(t *testing.T) {
	c, s3, b := fileFixture(t)
	ctx := context.Background()
	uploadTest(t, c, b, "protected.txt", "absent", []byte("original"), 200)
	name := "upload-ro-" + strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:10]))
	secret := "Upload-readonly-fixture-123"
	c.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": name, "secretKey": secret}, 200)
	c.call("POST", "admin/bindings", map[string]any{"user": name, "policy": "readonly"}, 200)
	defer func() {
		c.call("POST", "admin/bindings", map[string]any{"user": name, "policy": "readonly", "remove": true}, 200)
		c.call("DELETE", "admin/users/"+name, map[string]string{"confirmation": name}, 200)
	}()
	reader := loginTest(t, c.Origin, name, secret)
	var state objectwrite.FileState
	_ = json.Unmarshal(reader.call("GET", "objects/prepare?"+url.Values{"bucket": {b}, "key": {"protected.txt"}}.Encode(), nil, 200), &state)
	uploadTest(t, reader, b, "protected.txt", state.Revision, []byte("must not replace"), 403)
	object, err := s3.GetObject(ctx, b, "protected.txt", minio.GetObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer object.Close()
	body, err := io.ReadAll(object)
	if err != nil || string(body) != "original" {
		t.Fatal(string(body), err)
	}
}

func TestNormalJSONLimitUnchanged(t *testing.T) {
	request := httptest.NewRequest("POST", "http://ui.example/api", strings.NewReader(`{"name":"`+strings.Repeat("a", 64*1024)+`"}`))
	request.Header.Set("Content-Type", "application/json")
	var body struct {
		Name string `json:"name"`
	}
	if err := httpvalidation.Decode(httptest.NewRecorder(), request, &body); err == nil {
		t.Fatal("normal forms must stay bounded to 64 KiB")
	}
}

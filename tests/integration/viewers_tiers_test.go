package integration

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"

	apihttp "minio-web/api/src/http/api"
	archivessvc "minio-web/api/src/services/archives"
	bucketssvc "minio-web/api/src/services/buckets"
	runtimesvc "minio-web/api/src/services/runtime"
	"minio-web/api/src/services/securetoken"
	tierssvc "minio-web/api/src/services/tiers"
)

func zipped(text []byte) []byte {
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	z.Write(text)
	z.Close()
	return b.Bytes()
}
func TestGzipBoundsAndIntegrity(t *testing.T) {
	ctx := context.Background()
	if got, e := archivessvc.DecodeGzipText(ctx, zipped([]byte("текст\nlog"))); e != nil || got != "текст\nlog" {
		t.Fatal(got, e)
	}
	for _, data := range [][]byte{[]byte("not gzip"), zipped([]byte{255, 0}), zipped(bytes.Repeat([]byte{'a'}, int(archivessvc.MaxGzipText)+1)), zipped(bytes.Repeat([]byte{'\n'}, 200001))} {
		if _, e := archivessvc.DecodeGzipText(ctx, data); e == nil {
			t.Fatal("accepted invalid/oversized stream")
		}
	}
	data := zipped([]byte("log"))
	data[len(data)-8] ^= 1
	if _, e := archivessvc.DecodeGzipText(ctx, data); e == nil {
		t.Fatal("CRC ignored")
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, e := archivessvc.DecodeGzipText(ctx, zipped([]byte("log"))); e == nil {
		t.Fatal("cancel ignored")
	}
}
func TestTierSanitizationAndLifecycle(t *testing.T) {
	c, _ := madmin.NewTierS3("ARCHIVE", "private-access", "private-secret", "archive", madmin.S3Endpoint("https://example.com"))
	data, _ := json.Marshal(tierssvc.TierSummary(c))
	if strings.Contains(string(data), "private") || strings.Contains(string(data), "SecretKey") {
		t.Fatal(string(data))
	}
	c.S3.Endpoint = "https://user:secret@example.com/?password=x"
	if tierssvc.TierSummary(c).Endpoint != "" {
		t.Fatal("unsafe endpoint exposed")
	}
	input := bucketssvc.LifecycleInput{ID: "tier", Enabled: true, Tiering: &bucketssvc.LifecycleTiering{Current: "ARCHIVE", Days: 0, Noncurrent: "ARCHIVE", NoncurrentDays: 30}}
	rule, e := bucketssvc.ApplyLifecycleInput(lifecycle.Rule{}, input)
	if e != nil || rule.Transition.StorageClass != "ARCHIVE" || rule.NoncurrentVersionTransition.NoncurrentDays != 30 {
		t.Fatal(rule, e)
	}
	if got := bucketssvc.LifecycleRuleInput(rule); got.Tiering == nil || got.Tiering.Noncurrent != "ARCHIVE" {
		t.Fatal(got)
	}
	input.Tiering.Current = "lower"
	if _, e := bucketssvc.ApplyLifecycleInput(rule, input); e == nil {
		t.Fatal("lowercase accepted")
	}
	input.Tiering = &bucketssvc.LifecycleTiering{Current: "ARCHIVE", Date: "2035-02-30"}
	if _, e := bucketssvc.ApplyLifecycleInput(rule, input); e == nil {
		t.Fatal("invalid date")
	}
	input.Tiering = &bucketssvc.LifecycleTiering{Current: "ARCHIVE", Date: "2035-02-01"}
	if _, e := bucketssvc.ApplyLifecycleInput(rule, input); e != nil {
		t.Fatal(e)
	}
}
func TestGzipNativeIntegration(t *testing.T) {
	c, s3, bucket := fileFixture(t)
	data := zipped([]byte("line\nПривет\n"))
	_, e := s3.PutObject(context.Background(), bucket, "app.log.gz", bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{})
	if e != nil {
		t.Fatal(e)
	}
	raw := c.call("GET", "objects/gzip?"+url.Values{"bucket": {bucket}, "key": {"app.log.gz"}}.Encode(), nil, 200)
	var result struct {
		Text                 string
		Compressed, Expanded int
	}
	json.Unmarshal(raw, &result)
	if result.Text != "line\nПривет\n" || result.Compressed != len(data) {
		t.Fatal(string(raw))
	}
	c.call("PUT", "objects/gzip?"+url.Values{"bucket": {bucket}, "key": {"app.log.gz"}}.Encode(), nil, 405)
	head, _ := s3.StatObject(context.Background(), bucket, "app.log.gz", minio.StatObjectOptions{})
	if head.Size != int64(len(data)) {
		t.Fatal("changed source")
	}
}
func TestTiersNativeIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("isolated fixture")
	}
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
	cfg.Endpoints.Allowed["http://tier-minio:9000"] = true
	app := runtimesvc.New(cfg)
	defer app.Close()
	srv := httptest.NewServer(apihttp.NewHandler(app))
	defer srv.Close()
	c := loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret")
	name := "TIER" + strings.ToUpper(strings.NewReplacer("-", "A", "_", "B").Replace(securetoken.New()[:8]))
	bucket := "tier-" + strings.ToLower(name)
	source := bucket + "-source"
	remote, e := minio.New("127.0.0.1:19001", &minio.Options{Creds: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", "")})
	if e != nil {
		t.Fatal(e)
	}
	if e = remote.MakeBucket(context.Background(), bucket, minio.MakeBucketOptions{}); e != nil {
		t.Fatal(e)
	}
	c.call("POST", "admin/buckets", map[string]any{"name": source}, 200)
	defer func() {
		for obj := range remote.ListObjects(context.Background(), bucket, minio.ListObjectsOptions{Recursive: true, WithVersions: true}) {
			if obj.Err == nil {
				_ = remote.RemoveObject(context.Background(), bucket, obj.Key, minio.RemoveObjectOptions{VersionID: obj.VersionID})
			}
		}
		_ = remote.RemoveBucket(context.Background(), bucket)
	}()
	defer c.call("DELETE", "admin/buckets/"+source, map[string]any{"confirmation": source}, 200)
	body := map[string]any{"name": name, "type": "minio", "endpoint": "http://tier-minio:9000", "bucket": bucket, "prefix": "fixture/", "region": "us-east-1", "accessKey": "minio-web-test", "secretKey": "minio-web-test-secret"}
	c.call("POST", "admin/tiers", body, 200)
	var rows []tierssvc.TierView
	raw := c.call("GET", "admin/tiers", nil, 200)
	if strings.Contains(string(raw), "minio-web-test-secret") || strings.Contains(string(raw), "accessKey") {
		t.Fatal(string(raw))
	}
	json.Unmarshal(raw, &rows)
	var v tierssvc.TierView
	for _, row := range rows {
		if row.Name == name {
			v = row
		}
	}
	if v.Name == "" {
		t.Fatal(string(raw))
	}
	c.call("POST", "admin/tiers", map[string]any{"operation": "verify", "name": name}, 200)
	body["revision"] = v.Revision
	body["confirmation"] = name
	c.call("PUT", "admin/tiers", body, 200)
	body["revision"] = "stale"
	c.call("DELETE", "admin/tiers", body, 409)
	body["revision"] = v.Revision
	var field struct{ Value bucketssvc.LifecycleSnapshot }
	json.Unmarshal(c.call("GET", "admin/buckets/"+source+"/lifecycle", nil, 200), &field)
	rule := bucketssvc.LifecycleInput{ID: "transition", Enabled: false, Tiering: &bucketssvc.LifecycleTiering{Current: name, Days: 30}}
	c.call("POST", "admin/buckets/"+source+"/lifecycle", map[string]any{"revision": field.Value.Revision, "rule": rule}, 200)
	c.call("DELETE", "admin/tiers", body, 409)
	json.Unmarshal(c.call("GET", "admin/buckets/"+source+"/lifecycle", nil, 200), &field)
	c.call("DELETE", "admin/buckets/"+source+"/lifecycle", map[string]any{"revision": field.Value.Revision, "index": 0, "confirmation": "transition"}, 200)
	c.call("DELETE", "admin/tiers", body, 200)
	body["type"] = "s3"
	body["name"] = name + "S3"
	body["confirmation"] = name + "S3"
	c.call("POST", "admin/tiers", body, 200)
	json.Unmarshal(c.call("GET", "admin/tiers", nil, 200), &rows)
	for _, row := range rows {
		if row.Name == name+"S3" {
			body["revision"] = row.Revision
		}
	}
	c.call("POST", "admin/tiers", map[string]any{"name": name + "S3", "operation": "verify"}, 200)
	c.call("DELETE", "admin/tiers", body, 200)
	// Outbound destination must be explicitly allowed, even for administrators.
	body["endpoint"] = "https://not-allowed.example"
	c.call("POST", "admin/tiers", body, 400)
}

func TestViewersLeastPrivilegeIntegration(t *testing.T) {
	root, s3, bucket := fileFixture(t)
	data := zipped([]byte("private log"))
	s3.PutObject(context.Background(), bucket, "allowed.log.gz", bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{})
	s3.PutObject(context.Background(), bucket, "denied.log.gz", bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{})
	doc := map[string]any{"Version": "2012-10-17", "Statement": []any{map[string]any{"Effect": "Allow", "Action": []string{"s3:ListBucket"}, "Resource": []string{"arn:aws:s3:::" + bucket}}, map[string]any{"Effect": "Allow", "Action": []string{"s3:GetObject"}, "Resource": []string{"arn:aws:s3:::" + bucket + "/allowed.log.gz"}}, map[string]any{"Effect": "Allow", "Action": []string{"admin:ListTier"}, "Resource": []string{"*"}}}}
	var key struct{ AccessKey, SecretKey string }
	json.Unmarshal(root.call("POST", "admin/access-keys", map[string]any{"name": "viewer-limited", "policy": doc}, 200), &key)
	defer root.call("DELETE", "admin/access-keys/"+key.AccessKey, map[string]any{"confirmation": key.AccessKey}, 200)
	reader := loginTest(t, root.Origin, key.AccessKey, key.SecretKey)
	reader.call("GET", "objects/gzip?"+url.Values{"bucket": {bucket}, "key": {"allowed.log.gz"}}.Encode(), nil, 200)
	reader.call("GET", "objects/gzip?"+url.Values{"bucket": {bucket}, "key": {"denied.log.gz"}}.Encode(), nil, 403)
	reader.call("GET", "admin/tiers", nil, 200)
	reader.call("POST", "admin/tiers", map[string]any{"name": "ARCHIVE", "operation": "verify"}, 403)
}

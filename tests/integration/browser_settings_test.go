package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/sse"

	apihttp "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/presentation"
	bucketssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/buckets"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

func TestBrowserSettingsIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("isolated fixture")
	}
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
	app := runtimesvc.New(cfg)
	defer app.Close()
	srv := httptest.NewServer(apihttp.NewHandler(app))
	defer srv.Close()
	root := loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret")
	bucket := "browser-settings-" + strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:8]))
	root.call("POST", "admin/buckets", map[string]any{"name": bucket}, 200)
	client, _ := minio.New("127.0.0.1:19000", &minio.Options{Creds: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", ""), Region: "us-east-1"})
	defer client.RemoveBucket(context.Background(), bucket)

	get := func(kind string) (presentation.BrowserSetting, string) {
		t.Helper()
		raw := root.call("GET", "admin/buckets/"+bucket+"/"+kind, nil, 200)
		var f struct {
			Value    presentation.BrowserSetting `json:"value"`
			Revision string                      `json:"revision"`
		}
		if json.Unmarshal(raw, &f) != nil || !f.Value.Writable || f.Revision == "" {
			t.Fatal(string(raw))
		}
		return f.Value, f.Revision
	}
	// This pinned Community server does not implement per-bucket CORS writes.
	_, rev := get("cors")
	root.call("POST", "admin/buckets/"+bucket+"/cors", map[string]any{"revision": rev, "rule": bucketssvc.CorsRule{Origins: []string{"*"}, Methods: []string{"GET"}}}, 501)
	base := "admin/buckets/" + bucket + "/encryption"
	_, rev = get("encryption")
	root.call("PUT", base, map[string]any{"revision": rev, "algorithm": "AES256"}, 200)
	root.call("PUT", base, map[string]any{"revision": rev, "algorithm": "aws:kms"}, 409)
	value, rev := get("encryption")
	if value.Algorithm != "AES256" {
		t.Fatal(value)
	}
	_, err := client.PutObject(context.Background(), bucket, "encrypted", strings.NewReader("fixture"), 7, minio.PutObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	obj, err := client.GetObject(context.Background(), bucket, "encrypted", minio.GetObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(obj)
	obj.Close()
	if err != nil || string(data) != "fixture" {
		t.Fatal(err, string(data))
	}
	client.RemoveObject(context.Background(), bucket, "encrypted", minio.RemoveObjectOptions{})
	t.Logf("missing KMS code: %s", minio.ToErrorResponse(client.SetBucketEncryption(context.Background(), bucket, sse.NewConfigurationSSEKMS("missing-key"))).Code)
	root.call("PUT", base, map[string]any{"revision": rev, "algorithm": "aws:kms", "keyId": "missing-key"}, 409)
	root.call("PUT", base, map[string]any{"revision": rev, "algorithm": "aws:kms", "keyId": "fixture-key"}, 200)
	value, rev = get("encryption")
	if value.Algorithm != "aws:kms" || value.KeyID != "fixture-key" {
		t.Fatal(value)
	}
	root.call("PUT", base, map[string]any{"revision": rev, "algorithm": ""}, 400)
	root.call("PUT", base, map[string]any{"revision": rev, "algorithm": "", "confirmation": "remove-default"}, 200)
	value, _ = get("encryption")
	if value.Algorithm != "" {
		t.Fatal(value)
	}
}

func TestCORSValidation(t *testing.T) {
	for _, r := range []bucketssvc.CorsRule{{Origins: []string{"https://example.com/path"}, Methods: []string{"GET"}}, {Origins: []string{"*"}, Methods: []string{"OPTIONS"}}, {Origins: []string{"*"}, Methods: []string{"GET"}, Expose: []string{"*"}}, {Origins: []string{"*"}, Methods: []string{"GET"}, MaxAge: -1}} {
		if bucketssvc.ValidateCorsRule(r) == nil {
			t.Fatal(r)
		}
	}
}

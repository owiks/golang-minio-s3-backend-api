package integration

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	apihttp "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/presentation"
	bucketssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/buckets"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

func TestBucketTagsValidation(t *testing.T) {
	for _, input := range [][]bucketssvc.TagInput{nil, {{Key: "", Value: "x"}}, {{Key: "owner", Value: "one"}, {Key: "owner", Value: "two"}}, {{Key: "aws:owner", Value: "x"}}, {{Key: "owner", Value: "<script>"}}} {
		if _, err := bucketssvc.ValidateBucketTags(input); err == nil {
			t.Fatalf("accepted invalid tags: %+v", input)
		}
	}
	if _, err := bucketssvc.ValidateBucketTags([]bucketssvc.TagInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := bucketssvc.ValidateBucketTags([]bucketssvc.TagInput{{Key: "owner", Value: "team-one"}, {Key: "empty", Value: ""}}); err != nil {
		t.Fatal(err)
	}
}
func TestBucketSettingsIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("requires docker-compose.test.yml fixture")
	}
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
	app := runtimesvc.New(cfg)
	defer app.Close()
	srv := httptest.NewServer(apihttp.NewHandler(app))
	defer srv.Close()
	root := loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret")
	suffix := strings.NewReplacer("_", "a", "-", "b").Replace(strings.ToLower(securetoken.New()[:10]))
	bucket := "settings-test-" + suffix
	fixture, err := minio.New("127.0.0.1:19000", &minio.Options{Creds: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", ""), Region: "us-east-1", BucketLookup: minio.BucketLookupPath})
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.RemoveBucket(context.Background(), bucket)
	root.call("POST", "admin/buckets", map[string]string{"name": bucket}, 200)
	base := "admin/buckets/" + bucket + "/"
	fields := func() map[string]presentation.DetailField {
		t.Helper()
		var result map[string]json.RawMessage
		raw := root.call("GET", base+"details", nil, 200)
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		values := map[string]presentation.DetailField{}
		for _, kind := range []string{"policy", "quota", "tags"} {
			var f presentation.DetailField
			if err := json.Unmarshal(result[kind], &f); err != nil {
				t.Fatal(err)
			}
			if f.Error != "" || f.Revision == "" {
				t.Fatalf("unreadable %s: %s", kind, raw)
			}
			values[kind] = f
		}
		return values
	}
	put := func(kind string, body map[string]any, want int) presentation.DetailField {
		t.Helper()
		raw := root.call("PUT", base+kind, body, want)
		var result presentation.DetailField
		if want == 200 {
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatal(err)
			}
			if result.Error != "" || result.Revision == "" {
				t.Fatalf("write not readable: %s", raw)
			}
		}
		return result
	}
	initial := fields()
	document := map[string]any{"Version": "2012-10-17", "Statement": []any{map[string]any{"Sid": "BrowserEdit", "Effect": "Allow", "Principal": "*", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::" + bucket + "/public/*"}}}
	policy := put("policy", map[string]any{"revision": initial["policy"].Revision, "document": document}, 200)
	// Use the returned revision immediately: normalized server JSON must not conflict.
	put("policy", map[string]any{"revision": policy.Revision, "document": document}, 200)
	put("policy", map[string]any{"revision": initial["policy"].Revision, "document": nil}, 409)
	stored, err := fixture.GetBucketPolicy(context.Background(), bucket)
	if err != nil || !strings.Contains(stored, "BrowserEdit") {
		t.Fatal(stored, err)
	}
	current := fields()
	put("policy", map[string]any{"revision": current["policy"].Revision, "document": map[string]any{"Version": "2012-10-17"}}, 400)
	put("policy", map[string]any{"revision": current["policy"].Revision, "document": nil}, 200)
	stored, err = fixture.GetBucketPolicy(context.Background(), bucket)
	if err != nil || stored != "" {
		t.Fatal("policy not removed", err)
	}
	// Changes to a different field must not invalidate this field's revision.
	quota := put("quota", map[string]any{"revision": initial["quota"].Revision, "size": 1572864}, 200)
	put("quota", map[string]any{"revision": initial["quota"].Revision, "size": 0}, 409)
	put("quota", map[string]any{"revision": quota.Revision, "size": -1}, 400)
	put("quota", map[string]any{"revision": quota.Revision, "size": 1.5}, 400)
	put("quota", map[string]any{"revision": quota.Revision, "size": uint64(9007199254740992)}, 400)
	put("quota", map[string]any{"revision": quota.Revision, "size": 0}, 200)
	if quota.Value.(map[string]any)["size"] != float64(1572864) {
		t.Fatal("quota changed in conversion", quota)
	}
	tagField := put("tags", map[string]any{"revision": initial["tags"].Revision, "tags": []bucketssvc.TagInput{{Key: "owner", Value: "team-one"}, {Key: "empty", Value: ""}}}, 200)
	if tagField.Value.(map[string]any)["owner"] != "team-one" {
		t.Fatal("owner not persisted", tagField)
	}
	put("tags", map[string]any{"revision": initial["tags"].Revision, "tags": []bucketssvc.TagInput{}}, 409)
	put("tags", map[string]any{"revision": tagField.Revision, "tags": []bucketssvc.TagInput{{Key: "owner", Value: "one"}, {Key: "owner", Value: "two"}}}, 400)
	put("tags", map[string]any{"revision": tagField.Revision}, 400)
	put("tags", map[string]any{"revision": tagField.Revision, "tags": []bucketssvc.TagInput{}}, 200)
	final := fields()
	if final["policy"].Value != nil || len(final["tags"].Value.(map[string]any)) != 0 || final["quota"].Value.(map[string]any)["size"] != float64(0) {
		t.Fatalf("settings not cleared: %+v", final)
	}
}

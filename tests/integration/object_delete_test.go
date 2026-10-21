package integration

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	apihttp "minio-web/api/src/http/api"
	runtimesvc "minio-web/api/src/services/runtime"
	"minio-web/api/src/services/securetoken"
)

func TestObjectDeletionIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("isolated fixture")
	}
	ctx := context.Background()
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
	app := runtimesvc.New(cfg)
	defer app.Close()
	srv := httptest.NewServer(apihttp.NewHandler(app))
	defer srv.Close()
	root := loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret")
	client, _ := minio.New("127.0.0.1:19000", &minio.Options{Creds: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", ""), Region: "us-east-1"})
	admin, _ := madmin.New("127.0.0.1:19000", "minio-web-test", "minio-web-test-secret", false)
	suffix := strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:10]))
	plain, locked := "delete-"+suffix, "locked-delete-"+suffix
	user, policy := "delete-user-"+suffix, "delete-policy-"+suffix
	key := "a/Привет + # %.txt"
	sibling := key + "-neighbor"
	defer func() {
		admin.DetachPolicy(ctx, madmin.PolicyAssociationReq{User: user, Policies: []string{policy}})
		admin.RemoveUser(ctx, user)
		admin.RemoveCannedPolicy(ctx, policy)
		for _, bucket := range []string{plain, locked} {
			for row := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{WithVersions: true, Recursive: true}) {
				if row.Err == nil {
					client.PutObjectLegalHold(ctx, bucket, row.Key, minio.PutObjectLegalHoldOptions{VersionID: row.VersionID, Status: holdPointer(minio.LegalHoldDisabled)})
					client.RemoveObject(ctx, bucket, row.Key, minio.RemoveObjectOptions{VersionID: row.VersionID, GovernanceBypass: true})
				}
			}
			client.RemoveBucket(ctx, bucket)
		}
	}()
	root.call("POST", "admin/buckets", map[string]any{"name": plain}, 200)
	root.call("POST", "admin/buckets", map[string]any{"name": locked, "objectLock": true}, 200)
	put := func(bucket, key, text string, opts minio.PutObjectOptions) minio.UploadInfo {
		t.Helper()
		value, err := client.PutObject(ctx, bucket, key, strings.NewReader(text), int64(len(text)), opts)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	put(plain, key, "before", minio.PutObjectOptions{})
	put(plain, sibling, "neighbor", minio.PutObjectOptions{})
	endpoint := func(bucket, key string) string {
		return "admin/object-delete?" + url.Values{"bucket": {bucket}, "key": {key}}.Encode()
	}
	get := func(c *apiClient, bucket, key string) string {
		t.Helper()
		var body struct {
			Revision string `json:"revision"`
		}
		raw := c.call("GET", endpoint(bucket, key), nil, 200)
		if json.Unmarshal(raw, &body) != nil || body.Revision == "" {
			t.Fatal(string(raw))
		}
		return body.Revision
	}
	rev := get(root, plain, key)
	base := endpoint(plain, key)
	root.call("DELETE", base, map[string]any{"revision": rev, "confirmation": "wrong"}, 400)
	root.call("DELETE", base, map[string]any{"confirmation": key}, 400)
	root.call("DELETE", endpoint(plain, "a/"), map[string]any{"revision": rev, "confirmation": "a/"}, 400)
	root.call("DELETE", base+"&versionId=null", map[string]any{"revision": rev, "confirmation": key}, 400)
	originalCSRF := root.CSRF
	root.CSRF = "wrong"
	root.call("DELETE", base, map[string]any{"revision": rev, "confirmation": key}, 403)
	root.CSRF = originalCSRF
	put(plain, key, "updated-content", minio.PutObjectOptions{})
	root.call("DELETE", base, map[string]any{"revision": rev, "confirmation": key}, 409)
	rev = get(root, plain, key)
	root.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": user, "secretKey": "Delete-fixture-secret-123"}, 200)
	doc := json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:ListAllMyBuckets","s3:GetObject","s3:ListBucket"],"Resource":["arn:aws:s3:::*"]}]}`)
	root.call("POST", "admin/policies", map[string]any{"acknowledgeNonAtomicCreate": true, "name": policy, "document": doc}, 200)
	root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": policy}, 200)
	limited := loginTest(t, srv.URL, user, "Delete-fixture-secret-123")
	limited.call("DELETE", base, map[string]any{"revision": get(limited, plain, key), "confirmation": key}, 403)
	if _, err := client.StatObject(ctx, plain, key, minio.StatObjectOptions{}); err != nil {
		t.Fatal("denied DELETE removed object", err)
	}
	root.call("DELETE", base, map[string]any{"revision": rev, "confirmation": key}, 200)
	if _, err := client.StatObject(ctx, plain, key, minio.StatObjectOptions{}); minio.ToErrorResponse(err).Code != "NoSuchKey" {
		t.Fatal("object still visible", err)
	}
	root.call("GET", base, nil, 404)
	if _, err := client.StatObject(ctx, plain, sibling, minio.StatObjectOptions{}); err != nil {
		t.Fatal("sibling removed", err)
	}
	// A normal DELETE hides locked data with a marker; it never deletes the
	// protected version, even when the current caller is root.
	lockedVersion := put(locked, key, "protected", minio.PutObjectOptions{Mode: minio.Governance, RetainUntilDate: time.Now().Add(time.Hour), LegalHold: minio.LegalHoldEnabled})
	root.call("DELETE", endpoint(locked, key), map[string]any{"revision": get(root, locked, key), "confirmation": key}, 200)
	if _, err := client.StatObject(ctx, locked, key, minio.StatObjectOptions{}); minio.ToErrorResponse(err).Code != "NoSuchKey" {
		t.Fatal("missing marker", err)
	}
	if _, err := client.StatObject(ctx, locked, key, minio.StatObjectOptions{VersionID: lockedVersion.VersionID}); err != nil {
		t.Fatal("protected version was removed", err)
	}
	rows := 0
	markers := 0
	for row := range client.ListObjects(ctx, locked, minio.ListObjectsOptions{WithVersions: true, Recursive: true}) {
		if row.Err != nil {
			t.Fatal(row.Err)
		}
		rows++
		if row.IsDeleteMarker {
			markers++
		}
	}
	if rows != 2 || markers != 1 {
		t.Fatal(rows, markers)
	}
}

package integration

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	apihttp "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/presentation"
	objectssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

func TestNavigationAccessIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("isolated fixture only")
	}
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
	app := runtimesvc.New(cfg)
	defer app.Close()
	srv := httptest.NewServer(apihttp.NewHandler(app))
	defer srv.Close()
	root := loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret")
	snapshot := func(c *apiClient) ([]objectssvc.BucketRow, presentation.NavigationAccess) {
		t.Helper()
		var v struct {
			Buckets []objectssvc.BucketRow        `json:"buckets"`
			Access  presentation.NavigationAccess `json:"access"`
		}
		if err := json.Unmarshal(c.call("GET", "access", nil, 200), &v); err != nil {
			t.Fatal(err)
		}
		return v.Buckets, v.Access
	}
	_, all := snapshot(root)
	t.Log("root sections", all.Sections, "unknown", all.Unknown)
	for _, key := range []string{"buckets", "users", "groups", "policies", "access-keys", "temporary-access", "server", "batch", "diagnostics"} {
		if !slices.Contains(all.Sections, key) {
			t.Fatalf("root missing %s", key)
		}
	}
	suffix := strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:8]))
	bucket := "nav-visible-" + suffix
	hidden := "nav-hidden-" + suffix
	user := "nav-user-" + suffix
	policy := "nav-policy-" + suffix
	root.call("POST", "admin/buckets", map[string]any{"name": bucket}, 200)
	defer root.call("DELETE", "admin/buckets/"+bucket, map[string]any{"confirmation": bucket}, 200)
	root.call("POST", "admin/buckets", map[string]any{"name": hidden}, 200)
	defer root.call("DELETE", "admin/buckets/"+hidden, map[string]any{"confirmation": hidden}, 200)
	root.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": user, "secretKey": "Navigation-fixture-secret-123"}, 200)
	defer root.call("DELETE", "admin/users/"+user, map[string]any{"confirmation": user}, 200)
	doc := map[string]any{"Version": "2012-10-17", "Statement": []any{map[string]any{"Effect": "Allow", "Action": []string{"s3:ListAllMyBuckets"}, "Resource": []string{"arn:aws:s3:::*"}}, map[string]any{"Effect": "Allow", "Action": []string{"s3:ListBucket", "s3:GetBucketLocation"}, "Resource": []string{"arn:aws:s3:::" + bucket}}, map[string]any{"Effect": "Allow", "Action": []string{"s3:GetObject"}, "Resource": []string{"arn:aws:s3:::" + bucket + "/*"}}}}
	root.call("POST", "admin/policies", map[string]any{"acknowledgeNonAtomicCreate": true, "name": policy, "document": doc}, 200)
	defer root.call("DELETE", "admin/policies/"+policy, map[string]any{"confirmation": policy}, 200)
	root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": policy}, 200)
	attached := true
	defer func() {
		if attached {
			root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": policy, "remove": true}, 200)
		}
	}()
	limited := loginTest(t, srv.URL, user, "Navigation-fixture-secret-123")
	buckets, access := snapshot(limited)
	if len(buckets) != 1 || buckets[0].Name != bucket {
		t.Fatalf("unrelated buckets exposed: %v", buckets)
	}
	for _, key := range []string{"users", "groups", "policies", "server", "batch", "diagnostics", "incomplete-uploads"} {
		if slices.Contains(access.Sections, key) {
			t.Fatalf("denied section exposed %s", key)
		}
	}
	var listed []objectssvc.BucketRow
	json.Unmarshal(limited.call("GET", "buckets", nil, 200), &listed)
	if len(listed) != 1 || listed[0].Name != bucket {
		t.Fatal("bucket endpoint bypassed filter", listed)
	}
	limited.call("GET", "objects?bucket="+hidden, nil, 403)
	limited.call("GET", "admin/users", nil, 403)
	t.Run("group deny and disabled group", func(t *testing.T) {
		group := "nav-group-" + suffix
		groupPolicy := "nav-group-policy-" + suffix
		groupDoc := map[string]any{"Version": "2012-10-17", "Statement": []any{
			map[string]any{"Effect": "Allow", "Action": []string{"s3:ListBucket", "s3:GetBucketLocation"}, "Resource": []string{"arn:aws:s3:::" + hidden}},
			map[string]any{"Effect": "Deny", "Action": []string{"s3:ListBucket", "s3:GetBucketLocation", "s3:PutObject"}, "Resource": []string{"arn:aws:s3:::" + bucket, "arn:aws:s3:::" + bucket + "/*"}},
		}}
		root.call("POST", "admin/groups", map[string]any{"acknowledgeNonAtomicCreate": true, "name": group, "members": []string{user}}, 200)
		root.call("POST", "admin/policies", map[string]any{"acknowledgeNonAtomicCreate": true, "name": groupPolicy, "document": groupDoc}, 200)
		root.call("POST", "admin/bindings", map[string]any{"group": group, "policy": groupPolicy}, 200)
		defer func() {
			root.call("PUT", "admin/groups/"+group+"/status", map[string]any{"enabled": true}, 200)
			root.call("POST", "admin/bindings", map[string]any{"group": group, "policy": groupPolicy, "remove": true}, 200)
			root.call("POST", "admin/groups/"+group+"/members", map[string]any{"members": []string{user}, "remove": true}, 200)
			root.call("DELETE", "admin/groups/"+group, map[string]any{"confirmation": group}, 200)
			root.call("DELETE", "admin/policies/"+groupPolicy, map[string]any{"confirmation": groupPolicy}, 200)
		}()
		visible, _ := snapshot(limited)
		if len(visible) != 1 || visible[0].Name != hidden {
			t.Fatal("group deny was ignored", visible)
		}
		root.call("PUT", "admin/groups/"+group+"/status", map[string]any{"enabled": false}, 200)
		visible, _ = snapshot(limited)
		for _, b := range visible {
			if b.Name == hidden {
				t.Fatal("disabled group still granted access", visible)
			}
		}
		status, _ := limited.request("GET", limited.Base+"/objects?bucket="+bucket, nil)
		if status == 403 && len(visible) > 0 {
			t.Fatal("navigation disagrees with MinIO after disabling group", visible)
		}
	})
	var pair struct {
		AccessKey string `json:"accessKey"`
		SecretKey string `json:"secretKey"`
	}
	json.Unmarshal(root.call("POST", "admin/access-keys", map[string]any{"name": "nav-fixture", "policy": doc, "expiration": nil}, 200), &pair)
	defer root.call("DELETE", "admin/access-keys/"+pair.AccessKey, map[string]any{"confirmation": pair.AccessKey}, 200)
	derived := loginTest(t, srv.URL, pair.AccessKey, pair.SecretKey)
	derivedBuckets, derivedAccess := snapshot(derived)
	if len(derivedBuckets) != 1 || derivedBuckets[0].Name != bucket || slices.Contains(derivedAccess.Sections, "server") || slices.Contains(derivedAccess.Sections, "users") {
		t.Fatal("service account expanded to parent rights", derivedBuckets, derivedAccess)
	}
	t.Run("bucket access without global inventory permission", func(t *testing.T) {
		var state struct {
			Revision string `json:"revision"`
		}
		json.Unmarshal(root.call("GET", "admin/policies/"+policy, nil, 200), &state)
		scoped := map[string]any{"Version": "2012-10-17", "Statement": doc["Statement"].([]any)[1:]}
		root.call("PUT", "admin/policies/"+policy, map[string]any{"document": scoped, "revision": state.Revision}, 200)
		defer func() {
			json.Unmarshal(root.call("GET", "admin/policies/"+policy, nil, 200), &state)
			root.call("PUT", "admin/policies/"+policy, map[string]any{"document": doc, "revision": state.Revision}, 200)
		}()
		visible, _ := snapshot(limited)
		if len(visible) != 1 || visible[0].Name != bucket {
			t.Fatal("native discovery required global S3 list permission", visible)
		}
	})
	listOnly := "nav-list-only-" + suffix
	root.call("POST", "admin/policies", map[string]any{"acknowledgeNonAtomicCreate": true, "name": listOnly, "document": map[string]any{"Version": "2012-10-17", "Statement": []any{map[string]any{"Effect": "Allow", "Action": []string{"s3:ListAllMyBuckets"}, "Resource": []string{"arn:aws:s3:::*"}}}}}, 200)
	root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": listOnly}, 200)
	defer func() {
		root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": listOnly, "remove": true}, 200)
		root.call("DELETE", "admin/policies/"+listOnly, map[string]any{"confirmation": listOnly}, 200)
	}()
	root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": policy, "remove": true}, 200)
	attached = false
	buckets, _ = snapshot(limited)
	if len(buckets) != 0 {
		t.Fatal("revoked access retained buckets", buckets)
	}
}

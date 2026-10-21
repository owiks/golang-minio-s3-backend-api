package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/tags"

	settings "github.com/owiks/golang-minio-s3-backend-api/src/config"
	apihttp "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/presentation"
	bucketssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/buckets"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/revision"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

func testConfig() settings.Config {
	return settings.Default()
}
func TestSecurityGuards(t *testing.T) {
	app := runtimesvc.New(testConfig())
	defer app.Close()
	r := httptest.NewRequest("POST", "http://ui.example/api/sessions", strings.NewReader(`{"endpoint":"http://127.0.0.1"}`))
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	apihttp.ServeHTTP(app, w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	r = httptest.NewRequest("GET", "http://ui.example/api/sessions/unknown/admin/users", nil)
	w = httptest.NewRecorder()
	apihttp.ServeHTTP(app, w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	s := testsession.New(t, "http://fixture.example")
	if err := app.StoreSession(s); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("DELETE", "http://ui.example/api/sessions/"+s.SessionID(), nil)
	r.Header.Set("Origin", "http://ui.example")
	r.AddCookie(httpcommon.SessionCookie(s, r, false))
	w = httptest.NewRecorder()
	apihttp.ServeHTTP(app, w, r)
	if w.Code != 403 {
		t.Fatal("missing CSRF accepted", w.Code)
	}
	app.RemoveSession(s.SessionID())
	if httpcommon.SameOrigin(httptest.NewRequest("POST", "https://ui.example/api/sessions", nil), "") {
		t.Fatal("missing Origin accepted")
	}
}
func TestPolicyRevisionIsSemantic(t *testing.T) {
	a := []byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject","s3:ListBucket"],"Resource":["arn:aws:s3:::a","arn:aws:s3:::b"]}]}`)
	b := []byte(`{"Statement":[{"Resource":["arn:aws:s3:::b","arn:aws:s3:::a"],"Action":["s3:ListBucket","s3:GetObject"],"Effect":"Allow"}],"Version":"2012-10-17"}`)
	if revision.Of(a) != revision.Of(b) {
		t.Fatal("reordered IAM JSON triggered a false conflict")
	}
	if revision.Of(a) == revision.Of(bytes.ReplaceAll(b, []byte("Allow"), []byte("Deny"))) {
		t.Fatal("a real policy change was not detected")
	}
}
func TestSessionCookieScope(t *testing.T) {
	s := testsession.With(t, runtimesvc.Connection{}, settings.Config{Origin: "https://ui.example"}, nil)
	c := httpcommon.SessionCookie(s, httptest.NewRequest("GET", "http://internal/api", nil), false)
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/api/sessions/"+s.SessionID() || c.MaxAge < 3500 {
		t.Fatal("insecure or unscoped session cookie")
	}
	if httpcommon.SessionCookie(s, httptest.NewRequest("GET", "http://internal/api", nil), true).MaxAge != -1 {
		t.Fatal("logout cookie not expired")
	}
}

type apiClient struct {
	T                  *testing.T   `json:"-"`
	Http               *http.Client `json:"-"`
	Origin, Base, CSRF string       `json:"-"`
}

func (c *apiClient) request(method, route string, body any) (int, []byte) {
	c.T.Helper()
	var input io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		input = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.Origin+route, input)
	if err != nil {
		c.T.Fatal(err)
	}
	req.Header.Set("Origin", c.Origin)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", c.CSRF)
	res, err := c.Http.Do(req)
	if err != nil {
		c.T.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		c.T.Fatal(err)
	}
	assertResponseContract(c.T, method, req.URL.Path, res.StatusCode, res.Header.Get("Content-Type"), data)
	return res.StatusCode, data
}
func (c *apiClient) call(method, path string, body any, want int) []byte {
	c.T.Helper()
	status, data := c.request(method, c.Base+"/"+path, body)
	if status != want {
		c.T.Fatalf("%s %s: got %d want %d: %s", method, path, status, want, data)
	}
	return data
}
func loginTest(t *testing.T, origin, user, secret string) *apiClient {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &apiClient{T: t, Http: &http.Client{Jar: jar, Timeout: 40 * time.Second}, Origin: origin}
	status, data := c.request("POST", "/api/sessions", runtimesvc.Connection{Endpoint: "http://127.0.0.1:19000", AccessKey: user, SecretKey: secret, Region: "us-east-1"})
	if status != 201 {
		t.Fatalf("login %d: %s", status, data)
	}
	var reply struct {
		ID   string `json:"id"`
		CSRF string `json:"csrf"`
	}
	if json.Unmarshal(data, &reply) != nil {
		t.Fatal("invalid session")
	}
	c.Base = "/api/sessions/" + reply.ID
	c.CSRF = reply.CSRF
	return c
}

// Hard-coded loopback fixture only. This suite cannot target a user's storage.
func TestMinIOIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("set MINIO_INTEGRATION=1 with docker-compose.test.yml fixture running")
	}
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
	app := runtimesvc.New(cfg)
	defer app.Close()
	srv := httptest.NewServer(apihttp.NewHandler(app))
	defer srv.Close()
	root := loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret")
	suffix := strings.ToLower(securetoken.New()[:10])
	suffix = strings.ReplaceAll(suffix, "_", "a")
	suffix = strings.ReplaceAll(suffix, "-", "b")
	bucket := "api-test-" + suffix
	user := "api-user-" + suffix
	group := "api-group-" + suffix
	policy := "api-policy-" + suffix
	fixture, err := minio.New("127.0.0.1:19000", &minio.Options{Creds: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", ""), Region: "us-east-1", BucketLookup: minio.BucketLookupPath})
	if err != nil {
		t.Fatal(err)
	}
	admin, err := madmin.New("127.0.0.1:19000", "minio-web-test", "minio-web-test-secret", false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.DetachPolicy(context.Background(), madmin.PolicyAssociationReq{Group: group, Policies: []string{policy}})
		_ = admin.UpdateGroupMembers(context.Background(), madmin.GroupAddRemove{Group: group, Members: []string{user}, IsRemove: true})
		_ = admin.UpdateGroupMembers(context.Background(), madmin.GroupAddRemove{Group: group, IsRemove: true})
		_, _ = admin.DetachPolicy(context.Background(), madmin.PolicyAssociationReq{User: user, Policies: []string{policy}})
		_ = admin.RemoveUser(context.Background(), user)
		_ = admin.RemoveCannedPolicy(context.Background(), policy)
		_ = fixture.RemoveObject(context.Background(), bucket, "opaque/Привет + # %.txt", minio.RemoveObjectOptions{})
		_ = fixture.RemoveBucket(context.Background(), bucket)
	}()
	root.call("POST", "admin/buckets", map[string]string{"name": bucket}, 200)
	root.call("POST", "admin/buckets", map[string]string{"name": bucket}, 409)
	root.call("GET", "admin/bucket-usage", nil, 200)
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(root.call("GET", "admin/buckets/"+bucket+"/details", nil, 200), &settings); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"policy", "versioning", "quota", "locking", "tags"} {
		var field presentation.DetailField
		if err := json.Unmarshal(settings[key], &field); err != nil {
			t.Fatal(err)
		}
		if field.Error != "" {
			t.Fatalf("unexpected %s error: %s", key, field.Error)
		}
	}
	bucketTags, _ := tags.NewTags(map[string]string{"owner": "team-storage", "environment": "test"}, false)
	if err := fixture.SetBucketTagging(context.Background(), bucket, bucketTags); err != nil {
		t.Fatal(err)
	}
	bucketPolicy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::` + bucket + `/public/*"]}]}`
	if err := fixture.SetBucketPolicy(context.Background(), bucket, bucketPolicy); err != nil {
		t.Fatal(err)
	}
	if err := admin.SetBucketQuota(context.Background(), bucket, &madmin.BucketQuota{Size: 1024 * 1024, Type: madmin.HardQuota}); err != nil {
		t.Fatal(err)
	}
	details := root.call("GET", "admin/buckets/"+bucket+"/details", nil, 200)
	for _, want := range []string{"team-storage", "s3:GetObject", `"size":1048576`} {
		if !bytes.Contains(details, []byte(want)) {
			t.Fatalf("missing details %q: %s", want, details)
		}
	}

	_, err = fixture.PutObject(context.Background(), bucket, "opaque/Привет + # %.txt", strings.NewReader("Привет из Go API"), int64(len([]byte("Привет из Go API"))), minio.PutObjectOptions{ContentType: "text/plain", UserMetadata: map[string]string{"source": "go-test"}})
	if err != nil {
		t.Fatal(err)
	}
	root.call("DELETE", "admin/buckets/"+bucket, map[string]string{"confirmation": "wrong"}, 400)
	root.call("DELETE", "admin/buckets/"+bucket, map[string]string{"confirmation": bucket}, 409)
	raw := root.call("GET", "objects?bucket="+bucket+"&prefix=", nil, 200)
	if !bytes.Contains(raw, []byte(`"kind":"folder"`)) {
		t.Fatalf("folder mapping: %s", raw)
	}
	raw = root.call("GET", "objects?bucket="+bucket+"&prefix=opaque%2F", nil, 200)
	if !bytes.Contains(raw, []byte("Привет")) {
		t.Fatal("lost opaque key")
	}
	root.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": user, "secretKey": "User-secret-123"}, 200)
	root.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": user, "secretKey": "Must-not-overwrite"}, 409)
	raw = root.call("GET", "admin/users", nil, 200)
	if bytes.Contains(raw, []byte("secretKey")) || bytes.Contains(raw, []byte("User-secret-123")) {
		t.Fatal("user secret leaked")
	}
	doc := map[string]any{"Version": "2012-10-17", "Statement": []any{map[string]any{"Effect": "Allow", "Action": []string{"s3:ListAllMyBuckets"}, "Resource": []string{"arn:aws:s3:::*"}}}}
	doc["Statement"] = append(doc["Statement"].([]any), map[string]any{"Effect": "Allow", "Action": []string{"s3:GetBucketPolicy"}, "Resource": []string{"arn:aws:s3:::" + bucket}})
	root.call("POST", "admin/policies", map[string]any{"acknowledgeNonAtomicCreate": true, "name": policy, "document": doc}, 200)
	root.call("POST", "admin/policies", map[string]any{"acknowledgeNonAtomicCreate": true, "name": policy, "document": doc}, 409)
	raw = root.call("GET", "admin/policies/"+policy, nil, 200)
	var p struct {
		Revision string `json:"revision"`
	}
	_ = json.Unmarshal(raw, &p)
	root.call("PUT", "admin/policies/"+policy, map[string]any{"document": doc, "revision": "stale"}, 409)
	root.call("PUT", "admin/policies/"+policy, map[string]any{"document": doc, "revision": p.Revision}, 200)
	root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": policy}, 200)
	root.call("DELETE", "admin/policies/"+policy, map[string]string{"confirmation": policy}, 409)
	readonly := loginTest(t, srv.URL, user, "User-secret-123")
	readonly.call("GET", "admin/users", nil, 403)
	readonly.call("GET", "admin/bucket-usage", nil, 403)
	limitedDetails := readonly.call("GET", "admin/buckets/"+bucket+"/details", nil, 200)
	var partial map[string]json.RawMessage
	if err := json.Unmarshal(limitedDetails, &partial); err != nil {
		t.Fatal(err)
	}
	var policyField, quotaField presentation.DetailField
	json.Unmarshal(partial["policy"], &policyField)
	json.Unmarshal(partial["quota"], &quotaField)
	if policyField.Error != "" || policyField.Value == nil || quotaField.Error == "" {
		t.Fatalf("optional reads must preserve partial permissions: %s", limitedDetails)
	}

	readonly.call("PUT", "admin/buckets/"+bucket+"/policy", map[string]any{"revision": policyField.Revision, "document": nil}, 403)
	readonly.call("PUT", "admin/buckets/"+bucket+"/quota", map[string]any{"revision": "unused", "size": 0}, 403)
	readonly.call("PUT", "admin/buckets/"+bucket+"/tags", map[string]any{"revision": "unused", "tags": []bucketssvc.TagInput{}}, 403)
	readonly.call("PUT", "admin/buckets/"+bucket+"/versioning", map[string]any{"revision": "unused", "status": "Enabled"}, 403)
	readonly.call("PUT", "admin/buckets/"+bucket+"/locking", map[string]any{"revision": "unused", "enabled": true, "defaultRetention": nil}, 403)

	readonly.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": "not-created-" + suffix, "secretKey": "Not-created-secret"}, 403)
	readonly.call("POST", "admin/buckets", map[string]string{"name": "not-created-" + suffix}, 403)
	root.call("POST", "admin/groups", map[string]any{"acknowledgeNonAtomicCreate": true, "name": group, "members": []string{user}}, 200)
	root.call("POST", "admin/groups", map[string]any{"acknowledgeNonAtomicCreate": true, "name": group, "members": []string{user}}, 409)
	root.call("DELETE", "admin/groups/"+group, map[string]string{"confirmation": group}, 409)
	root.call("POST", "admin/bindings", map[string]any{"group": group, "policy": policy}, 200)
	raw = root.call("GET", "admin/bindings?user="+user, nil, 200)
	if !bytes.Contains(raw, []byte(group)) {
		t.Fatalf("missing inherited policy: %s", raw)
	}
	root.call("PUT", "admin/groups/"+group+"/status", map[string]bool{"enabled": false}, 200)
	root.call("PUT", "admin/groups/"+group+"/status", map[string]bool{"enabled": true}, 200)
	root.call("POST", "admin/bindings", map[string]any{"group": group, "policy": policy, "remove": true}, 200)
	root.call("POST", "admin/groups/"+group+"/members", map[string]any{"members": []string{user}, "remove": true}, 200)
	root.call("DELETE", "admin/groups/"+group, map[string]string{"confirmation": group}, 200)
	// An IAM-only operator can connect even without s3:ListAllMyBuckets.
	raw = root.call("GET", "admin/policies/"+policy, nil, 200)
	_ = json.Unmarshal(raw, &p)
	iamOnly := map[string]any{"Version": "2012-10-17", "Statement": []any{map[string]any{"Effect": "Allow", "Action": []string{"admin:ListUsers"}, "Resource": []string{"arn:aws:s3:::*"}}}}
	root.call("PUT", "admin/policies/"+policy, map[string]any{"document": iamOnly, "revision": p.Revision}, 200)
	iam := loginTest(t, srv.URL, user, "User-secret-123")
	iam.call("GET", "admin/users", nil, 200)
	if data := iam.call("GET", "buckets", nil, 200); strings.TrimSpace(string(data)) != "[]" {
		t.Fatal("IAM-only operator saw unrelated buckets", string(data))
	}
	root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": policy, "remove": true}, 200)
	root.call("DELETE", "admin/policies/"+policy, map[string]string{"confirmation": policy}, 200)
	root.call("PUT", "admin/users/"+user+"/status", map[string]bool{"enabled": false}, 200)
	root.call("PUT", "admin/users/"+user+"/status", map[string]bool{"enabled": true}, 200)
	root.call("DELETE", "admin/users/minio-web-test", map[string]string{"confirmation": "minio-web-test"}, 409)
	root.call("DELETE", "admin/users/"+user, map[string]string{"confirmation": user}, 200)
	_ = fixture.RemoveObject(context.Background(), bucket, "opaque/Привет + # %.txt", minio.RemoveObjectOptions{})
	root.call("DELETE", "admin/buckets/"+bucket, map[string]string{"confirmation": bucket}, 200)
	status, data := root.request("DELETE", root.Base, nil)
	if status != 200 {
		t.Fatal(status, string(data))
	}
	root.call("GET", "buckets", nil, 401)
}

func TestUsageRowsNeverInventZeroStats(t *testing.T) {
	names := []minio.BucketInfo{{Name: "visible"}, {Name: "not-scanned"}}
	info := madmin.DataUsageInfo{BucketsUsage: map[string]madmin.BucketUsageInfo{"visible": {Size: 42, ObjectsCount: 3}, "hidden": {Size: 99}}}
	if got := bucketssvc.UsageRows(info, names); got.UpdatedAt != nil || len(got.Buckets) != 0 {
		t.Fatal("uninitialized scanner data must be unknown")
	}
	info.LastUpdate = time.Now()
	got := bucketssvc.UsageRows(info, names)
	if len(got.Buckets) != 1 || got.Buckets["visible"].Size != 42 || got.Buckets["visible"].Objects != 3 {
		t.Fatalf("unexpected statistics: %+v", got)
	}
	if _, ok := got.Buckets["not-scanned"]; ok {
		t.Fatal("missing entry must not become zero")
	}
}

package integration

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	apihttp "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

func TestAccessVersionScopeAndAuthentication(t *testing.T) {
	app := runtimesvc.New(testConfig())
	defer app.Close()
	endpoint, _ := url.Parse("http://minio:9000")
	first := testsession.New(t, endpoint.String())
	second := testsession.New(t, endpoint.String())
	otherURL, _ := url.Parse("http://other:9000")
	other := testsession.New(t, otherURL.String())
	initial := app.AccessRevision(first)
	otherInitial := app.AccessRevision(other)
	app.InvalidateAccess(second)
	if app.AccessRevision(first) == initial || app.AccessRevision(other) != otherInitial {
		t.Fatal("revision must be shared only by sessions of the same endpoint")
	}
	defer func() { app.RemoveSession(first.SessionID()) }()
	if err := app.StoreSession(first); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://ui.example/api/sessions/"+first.SessionID()+"/access-version", nil)
	w := httptest.NewRecorder()
	apihttp.ServeHTTP(app, w, r)
	if w.Code != 401 {
		t.Fatal("missing cookie accepted", w.Code)
	}
	r.AddCookie(httpcommon.SessionCookie(first, r, false))
	w = httptest.NewRecorder()
	apihttp.ServeHTTP(app, w, r)
	var v map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	if w.Code != 200 || v["revision"] != app.AccessRevision(first) || len(v) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, operation := range []struct{ method, path string }{
		{"POST", "admin/users"}, {"PUT", "admin/users/alice/status"}, {"POST", "admin/groups/editors/members"}, {"PUT", "admin/groups/editors/status"}, {"PUT", "admin/policies/read"}, {"POST", "admin/bindings"}, {"DELETE", "admin/access-keys/key"},
	} {
		if !apihttp.IamMutation(operation.method, operation.path) || apihttp.IamMutation("GET", operation.path) {
			t.Fatal(operation)
		}
	}
	if apihttp.IamMutation("POST", "admin/users/alice/status") || apihttp.IamMutation("DELETE", "admin/groups/team/unknown") {
		t.Fatal("unregistered mutation invalidates access")
	}

	for _, route := range []string{"action-access", "objects/delete", "admin/buckets", "admin/policy-summaries", "admin/group-summaries"} {
		if apihttp.IamMutation("POST", route) {
			t.Fatal(route)
		}
	}
}
func TestAccessVersionMutationIntegration(t *testing.T) {
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
	observer := loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret")
	version := func() string {
		t.Helper()
		var v struct {
			Revision string `json:"revision"`
		}
		if err := json.Unmarshal(observer.call("GET", "access-version", nil, 200), &v); err != nil {
			t.Fatal(err)
		}
		return v.Revision
	}
	previous := version()
	root.call("GET", "admin/policies", nil, 200)
	if version() != previous {
		t.Fatal("read invalidated rights")
	}
	root.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": "bad", "secretKey": "short"}, 400)
	if version() != previous {
		t.Fatal("failed mutation invalidated rights")
	}
	user := "revision-user-" + strings.ToLower(securetoken.New()[:8])
	root.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": user, "secretKey": "Revision-fixture-secret"}, 200)
	defer root.call("DELETE", "admin/users/"+user, map[string]any{"confirmation": user}, 200)
	if version() == previous {
		t.Fatal("committed mutation not visible to another session")
	}
	var snapshot struct {
		Revision string `json:"revision"`
	}
	_ = json.Unmarshal(observer.call("GET", "access", nil, 200), &snapshot)
	if snapshot.Revision != version() {
		t.Fatal("snapshot missing invalidation version")
	}
}

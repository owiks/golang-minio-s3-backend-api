package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/doc"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

func TestEveryDocumentedOperationResolvesThroughRegistry(t *testing.T) {
	document := doc.Build(Catalog())
	total := 0
	for path, methods := range document.Paths.Map() {
		concrete := strings.NewReplacer("{sessionID}", "fixture-session", "{name}", "fixture-name", "{id}", "fixture-id", "{ticket}", "fixture-ticket", "{operation}", "fixture-operation", "{part}", "1", "{connectionID}", "fixture-connection", "{tokenID}", "fixture-token").Replace(path)
		for method := range methods.Operations() {
			total++
			m := registry.Match(strings.ToUpper(method), concrete)
			if !m.MethodAllowed || m.Template != path || m.Operation.Handler == nil {
				t.Fatalf("documentation/dispatch mismatch: %s %s", method, path)
			}
			if m.Route.Public && m.Params["sessionID"] != "" {
				t.Fatal("session operation became public", path)
			}
		}
	}
	if total != 150 {
		t.Fatal("unexpected operation count", total)
	}
}
func TestOperationBudgetsAndTransferAdmissionAreExplicit(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		long         bool
	}{
		{"POST", "objects/select", true}, {"POST", "objects/multipart", true}, {"GET", "objects/multipart/operation", false}, {"PUT", "objects/multipart/operation/parts/1", true}, {"POST", "objects/multipart/operation/parts/1/sign", false}, {"POST", "objects/multipart/operation/complete", true}, {"DELETE", "objects/multipart/operation", true},
		{"PUT", "objects/upload", true}, {"POST", "objects/transfer", true}, {"POST", "objects/import", true}, {"POST", "objects/content-search", true}, {"POST", "objects/checksum", true}, {"GET", "objects/checksum", false}, {"PUT", "objects/properties", true}, {"GET", "objects/properties", false}, {"GET", "objects/archive/ticket", true}, {"DELETE", "objects/archive/ticket", false}, {"GET", "objects/archive-status/ticket", false},
	} {
		m := matchedOperation(t, tc.method, tc.path)
		if m.Operation.Long != tc.long || tc.long && m.NeedsMutationLock() {
			t.Fatal("wrong execution budget", tc)
		}
	}
	for _, path := range []string{"/api/sessions/id/fake/objects/upload", "/api/sessions/id/objects/archive/ticket/extra"} {
		if registry.Match("PUT", path).Found {
			t.Fatal("unknown path gained a transfer budget", path)
		}
	}
}
func TestRegistryRejectsMethodsBeforeControllers(t *testing.T) {
	cfg := config.Default()
	cfg.Origin = "http://ui.example"
	app := runtime.New(cfg)
	defer app.Close()
	endpoint, _ := url.Parse("http://fixture.example:9000")
	s := testsession.New(t, endpoint.String())
	if err := app.StoreSession(s); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(app)
	revision := app.AccessRevision(s)
	for _, tc := range []struct {
		method, path string
		status       int
		allow        string
		auth         bool
	}{
		{"POST", "/api/sessions/fixture/objects/info", 405, "GET", true},
		{"PUT", "/api/sessions/fixture/admin/buckets/name/details", 405, "GET", true},
		{"GET", "/api/sessions/fixture/admin/buckets/name/details/extra", 404, "", true},
		{"GET", "/api/sessions/fixture/unknown/objects/upload", 404, "", true},
		{"GET", "/api/sessions/fixture/unknown", 401, "", false},
		{"POST", "/api/receive", 405, "GET, PUT", false},
		{"PUT", "/api/shared-folder/file/secret", 405, "GET, HEAD", false},
		{"GET", "/health/extra", 404, "", false},
	} {
		path := strings.ReplaceAll(tc.path, "/api/sessions/fixture", "/api/sessions/"+s.SessionID())
		r := httptest.NewRequest(tc.method, "http://ui.example"+path, nil)
		if tc.auth {
			r.AddCookie(httpcommon.SessionCookie(s, r, false))
			r.Header.Set("Origin", cfg.Origin)
			r.Header.Set("X-CSRF-Token", s.CSRFToken())
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status || w.Header().Get("Allow") != tc.allow {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
		if app.AccessRevision(s) != revision {
			t.Fatal("rejected request invalidated access")
		}
		if app.ActiveTransfers() != 0 {
			t.Fatal("invalid route consumed transfer admission")
		}
	}
}
func TestMatchedOperationDoesNotBypassAuthentication(t *testing.T) {
	app := runtime.New(config.Default())
	defer app.Close()
	for _, route := range Catalog() {
		if route.Public {
			continue
		}
		for _, op := range route.Operations {
			path := strings.NewReplacer("{sessionID}", "missing", "{name}", "name", "{id}", "job", "{ticket}", "ticket").Replace(route.Template(op))
			w := httptest.NewRecorder()
			ServeHTTP(app, w, httptest.NewRequest(op.Method, path, nil))
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("unprotected operation: %s %s: %d", op.Method, path, w.Code)
			}
		}
	}
}

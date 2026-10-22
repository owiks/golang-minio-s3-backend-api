package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operationpolicy"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	uploadinvite "github.com/owiks/golang-minio-s3-backend-api/src/services/uploads/invite"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

func TestOperatorActionsDescribeEffectsRatherThanHTTPMethods(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		want         operationpolicy.Action
	}{
		{"POST", "action-access", 0}, {"POST", "objects/select", 0}, {"POST", "objects/checksum", 0},
		{"POST", "objects/content-search", 0}, {"POST", "objects/archive", 0},
		{"POST", "objects/folder-shares", 0}, {"POST", "tokens/id/rotate", 0},
		{"DELETE", "connections/id", 0}, {"DELETE", "tokens/id", 0},
		{"DELETE", "objects/multipart/id", 0}, {"POST", "admin/incomplete-uploads/abort", 0},
		{"DELETE", "objects/archive/id", 0}, {"DELETE", "admin/batch-jobs/id", 0},
		{"PUT", "objects/upload", operationpolicy.Upload}, {"PUT", "objects/content", operationpolicy.Upload},
		{"POST", "objects/folders", operationpolicy.Upload}, {"POST", "objects/import", operationpolicy.Upload},
		{"POST", "objects/zip", operationpolicy.Upload}, {"POST", "objects/transfer", operationpolicy.Upload},
		{"POST", "objects/invites", operationpolicy.Upload}, {"POST", "objects/multipart", operationpolicy.Upload},
		{"PUT", "objects/multipart/id/parts/1", operationpolicy.Upload},
		{"POST", "objects/multipart/id/parts/1/sign", operationpolicy.Upload}, {"POST", "objects/multipart/id/complete", operationpolicy.Upload},
		{"DELETE", "admin/object-delete", operationpolicy.Delete}, {"DELETE", "objects/folder-delete", operationpolicy.Delete},
		{"PUT", "objects/properties", operationpolicy.Write}, {"PUT", "admin/object-versions/hold", operationpolicy.Admin},
		{"DELETE", "admin/buckets/data", operationpolicy.Admin | operationpolicy.Delete},
		{"POST", "admin/users", operationpolicy.Admin}, {"PUT", "admin/server-config", operationpolicy.Admin},
	} {
		m := matchedOperation(t, tc.method, tc.path)
		if got := routes.OperatorAction(m.Route, m.Operation); got != tc.want {
			t.Fatal(tc, got)
		}
	}
	m := registry.Match("PUT", "/api/receive")
	if routes.OperatorAction(m.Route, m.Operation) != operationpolicy.Upload {
		t.Fatal("delegated upload bypasses policy")
	}
	if routes.OperatorAction(routes.APIRoute{}, routes.Operation{Method: "POST", Controller: "FutureCommand"}) != operationpolicy.Write {
		t.Fatal("future command bypasses read-only mode")
	}
}

func TestMaintenanceBlocksCommandsBeforeBodyAndUpstream(t *testing.T) {
	var upstreamCalls atomic.Int32
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(context.Context, string, string) (net.Conn, error) {
		upstreamCalls.Add(1)
		return nil, errors.New("unexpected upstream request")
	}
	cfg := config.Default()
	cfg.Operations.ReadOnly = true
	app := runtime.New(cfg)
	defer app.Close()
	s := testsession.With(t, runtime.Connection{Endpoint: "http://fixture.example"}, cfg, transport)
	if err := app.StoreSession(s); err != nil {
		t.Fatal(err)
	}
	for _, language := range []string{"en", "ru"} {
		for _, tc := range []struct{ method, path string }{
			{"PUT", "objects/upload"}, {"POST", "objects/multipart"}, {"PUT", "objects/multipart/id/parts/1"},
			{"POST", "objects/multipart/id/parts/1/sign"}, {"POST", "objects/multipart/id/complete"},
			{"POST", "objects/transfer"}, {"DELETE", "objects/folder-delete"}, {"DELETE", "admin/object-delete"},
			{"PUT", "objects/properties"}, {"POST", "admin/users"}, {"PUT", "admin/server-config"},
		} {
			r := operatorRequest(s, tc.method, tc.path, "invalid body")
			r.Header.Set("Accept-Language", language)
			w := httptest.NewRecorder()
			ServeHTTP(app, w, r)
			var value struct {
				Error struct{ Code, Message string }
			}
			if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
				t.Fatal(err)
			}
			if w.Code != 503 || value.Error.Code != "OperationDisabled" || value.Error.Message == "" || strings.Contains(value.Error.Message, "api.operation_disabled") || w.Header().Get("Content-Language") != language {
				t.Fatal(tc, w.Code, w.Body.String())
			}
		}
	}
	// Read POSTs and abort operations still reach their ordinary validation.
	for _, tc := range []struct{ method, path string }{
		{"POST", "objects/select"}, {"POST", "objects/archive"}, {"POST", "action-access"},
		{"DELETE", "objects/multipart/missing"}, {"DELETE", "objects/archive/missing"},
		{"POST", "tokens/missing/rotate"},
	} {
		w := httptest.NewRecorder()
		ServeHTTP(app, w, operatorRequest(s, tc.method, tc.path, "{}"))
		if w.Code == 503 || w.Code >= 500 || strings.Contains(w.Body.String(), "OperationDisabled") {
			t.Fatal("read/control operation disabled", tc, w.Code, w.Body.String())
		}
	}
	if upstreamCalls.Load() != 0 {
		t.Fatal("maintenance contacted upstream", upstreamCalls.Load())
	}
	// Caller authentication still wins over operator availability.
	r := httptest.NewRequest("PUT", "/api/sessions/missing/objects/upload", nil)
	w := httptest.NewRecorder()
	ServeHTTP(app, w, r)
	if w.Code != 401 {
		t.Fatal("maintenance bypassed authentication", w.Code)
	}
}

func TestDeleteSwitchAllowsCopyButRejectsMoveBeforeStorage(t *testing.T) {
	cfg := config.Default()
	cfg.Operations.DisableDeletes = true
	app := runtime.New(cfg)
	defer app.Close()
	s := testsession.New(t, "http://fixture.example")
	if err := app.StoreSession(s); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body string
		want int
	}{{`{"move":true}`, 503}, {`{"move":false}`, 400}} {
		w := httptest.NewRecorder()
		ServeHTTP(app, w, operatorRequest(s, "POST", "objects/transfer", tc.body))
		if w.Code != tc.want {
			t.Fatal(tc, w.Code, w.Body.String())
		}
	}
}

func operatorRequest(s *runtime.Session, method, path, body string) *http.Request {
	r := httptest.NewRequest(method, "http://ui/api/sessions/"+s.SessionID()+"/"+path, strings.NewReader(body))
	r.Header.Set("Origin", "http://ui")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", s.CSRFToken())
	r.AddCookie(&http.Cookie{Name: "minio_session_" + s.SessionID(), Value: s.CookieSnapshot().Token})
	return r
}

func TestMaintenanceBlocksDelegatedUploadAndKeepsLogout(t *testing.T) {
	cfg := config.Default()
	cfg.Operations.ReadOnly = true
	app := runtime.New(cfg)
	defer app.Close()
	s := testsession.New(t, "http://fixture.example")
	if err := app.StoreSession(s); err != nil {
		t.Fatal(err)
	}
	grant := uploadinvite.New(uploadinvite.Config{Owner: s.SessionID(), Token: "separate-invite-secret", Bucket: "data", Prefix: "inbox/", Expires: time.Now().Add(time.Minute), MaxSize: 10, MaxFiles: 1})
	if err := app.Invites.Add(grant); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method string
		want   int
	}{{"GET", 200}, {"PUT", 503}} {
		r := httptest.NewRequest(tc.method, "http://ui/api/receive?name=file.txt", strings.NewReader("data"))
		r.Header.Set("Authorization", "Bearer "+grant.Token())
		r.Header.Set("Origin", "http://ui")
		r.Header.Set("X-Upload-Size", "4")
		w := httptest.NewRecorder()
		ServeHTTP(app, w, r)
		if w.Code != tc.want {
			t.Fatal(tc, w.Code, w.Body.String())
		}
	}
	r := operatorRequest(s, "DELETE", "", "")
	r.URL.Path = strings.TrimSuffix(r.URL.Path, "/")
	w := httptest.NewRecorder()
	ServeHTTP(app, w, r)
	if w.Code != 200 {
		t.Fatal("cannot revoke session during maintenance", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	ServeHTTP(app, w, operatorRequest(s, "GET", "credential", ""))
	if w.Code != 401 {
		t.Fatal("logout failed to revoke access", w.Code, w.Body.String())
	}
}

func TestDeleteSwitchAlsoBlocksNewBatchExpiration(t *testing.T) {
	cfg := config.Default()
	cfg.Operations.DisableDeletes = true
	app := runtime.New(cfg)
	defer app.Close()
	s := testsession.New(t, "http://fixture.example")
	if err := app.StoreSession(s); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body string
		want int
	}{{`{"type":"expire"}`, 503}, {`{"type":"keyrotate"}`, 400}} {
		w := httptest.NewRecorder()
		ServeHTTP(app, w, operatorRequest(s, "POST", "admin/batch-jobs", tc.body))
		if w.Code != tc.want {
			t.Fatal(tc, w.Code, w.Body.String())
		}
	}
}

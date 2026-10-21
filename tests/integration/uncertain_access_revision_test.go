package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/credentials"

	apihttp "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/revision"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

func TestIAMHTTPRevisionFollowsCommandEffectNotError(t *testing.T) {
	policy := json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::fixture/*"]}]}`)
	for _, tc := range []struct {
		name, method, path, response string
		body                         any
		status                       int
		changed                      bool
	}{
		{"create user lost response", "POST", "admin/users", "lost", map[string]any{"acknowledgeNonAtomicCreate": true, "name": "new-user", "secretKey": "fixture-secret"}, 502, true},
		{"user status lost response", "PUT", "admin/users/alice/status", "lost", map[string]bool{"enabled": false}, 502, true},
		{"group status lost response", "PUT", "admin/groups/editors/status", "lost", map[string]bool{"enabled": false}, 502, true},
		{"policy lost response", "PUT", "admin/policies/reader", "lost", map[string]any{"document": policy, "revision": revision.Of(policy)}, 502, true},
		{"binding lost response", "POST", "admin/bindings", "lost", map[string]any{"user": "alice", "policy": "reader"}, 502, true},
		{"key deletion lost response", "DELETE", "admin/access-keys/child-key", "lost", map[string]any{"confirmation": "child-key"}, 502, true},
		{"key creation invalid response", "POST", "admin/access-keys", "invalid", map[string]any{"name": "child", "policy": nil}, 502, true},
		{"applied but proxy failed", "PUT", "admin/users/alice/status", "proxy", map[string]bool{"enabled": false}, 502, true},
		{"applied oversized reply", "PUT", "admin/users/alice/status", "oversized", map[string]bool{"enabled": false}, 502, true},
		{"successful status change", "PUT", "admin/users/alice/status", "ok", map[string]bool{"enabled": false}, 200, true},
		{"explicit native refusal", "PUT", "admin/users/alice/status", "denied", map[string]bool{"enabled": false}, 403, false},
		{"validation failed", "POST", "admin/users", "unused", map[string]string{"name": "new-user", "secretKey": "short"}, 400, false},
		{"read failed before write", "POST", "admin/users", "preflight", map[string]string{"name": "new-user", "secretKey": "fixture-secret"}, 403, false},
		{"ordinary read", "GET", "admin/users/alice", "unused", nil, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var commands, applied atomic.Int32
			s := actionCheckSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					switch {
					case strings.HasSuffix(r.URL.Path, "/user-info"):
						if tc.response == "preflight" {
							httpcommon.WriteJSON(w, r, 403, map[string]string{"Code": "AccessDenied"})
						} else if tc.method == "GET" {
							httpcommon.WriteJSON(w, r, 200, map[string]any{"status": "enabled"})
						} else {
							httpcommon.WriteJSON(w, r, 404, map[string]string{"Code": "XMinioAdminNoSuchUser"})
						}
					case strings.HasSuffix(r.URL.Path, "/info-canned-policy"):
						_, _ = w.Write(policy)
					default:
						t.Errorf("unexpected read: %s", r.URL.Path)
						w.WriteHeader(400)
					}
					return
				}
				commands.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				if tc.response == "denied" {
					httpcommon.WriteJSON(w, r, 403, map[string]string{"Code": "XMinioAdminAccessDenied", "Message": "PRIVATE-UPSTREAM"})
					return
				}
				applied.Add(1)
				switch tc.response {
				case "lost":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
				case "proxy":
					httpcommon.WriteJSON(w, r, 502, map[string]string{"Code": "InternalError", "Message": "PRIVATE-UPSTREAM"})
				case "oversized":
					w.(http.Flusher).Flush()
					_, _ = w.Write(bytes.Repeat([]byte("x"), (8<<20)+1))
				case "invalid":
					_, _ = w.Write([]byte("PRIVATE-NOT-ENCRYPTED"))
				default:
					w.WriteHeader(200)
				}
			})
			app := runtimesvc.New(testConfig())
			defer app.Close()
			if err := app.StoreSession(s); err != nil {
				t.Fatal(err)
			}
			observer := testsession.New(t, runtimesvc.NewClients(s).InternalEndpoint().String())
			if err := app.StoreSession(observer); err != nil {
				t.Fatal(err)
			}
			other := testsession.New(t, "http://another-storage.invalid:9000")
			previous, otherPrevious := app.AccessRevision(observer), app.AccessRevision(other)
			payload, _ := json.Marshal(tc.body)
			r := httptest.NewRequest(tc.method, "http://ui.example/api/sessions/"+s.SessionID()+"/"+tc.path, bytes.NewReader(payload))
			r.Header.Set("Origin", "http://ui.example")
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", s.CSRFToken())
			r.AddCookie(httpcommon.SessionCookie(s, r, false))
			w := httptest.NewRecorder()
			apihttp.NewHandler(app).ServeHTTP(w, r)
			if w.Code != tc.status || strings.Contains(w.Body.String(), "PRIVATE") {
				t.Fatal("HTTP result changed or raw error exposed", w.Code, w.Body.String())
			}
			if (app.AccessRevision(observer) != previous) != tc.changed || app.AccessRevision(s) != app.AccessRevision(observer) || app.AccessRevision(other) != otherPrevious {
				t.Fatal("incorrect revision or endpoint scope", tc.changed, app.AccessRevision(observer), previous)
			}
			poll := httptest.NewRequest("GET", "http://ui.example/api/sessions/"+observer.SessionID()+"/access-version", nil)
			poll.AddCookie(httpcommon.SessionCookie(observer, poll, false))
			version := httptest.NewRecorder()
			apihttp.ServeHTTP(app, version, poll)
			var reported struct{ Revision string }
			if version.Code != 200 || json.Unmarshal(version.Body.Bytes(), &reported) != nil || reported.Revision != app.AccessRevision(observer) {
				t.Fatal("another session cannot observe invalidation", version.Code, version.Body.String())
			}
			wantCommands := int32(1)
			if tc.response == "unused" || tc.response == "preflight" {
				wantCommands = 0
			}
			if commands.Load() != wantCommands || tc.changed && applied.Load() != 1 {
				t.Fatal("command replayed or scenario not applied", commands.Load(), applied.Load())
			}
		})
	}
}

func TestIAMAppliedCommandLostResponseNativeIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("isolated MinIO fixture required")
	}
	target, _ := url.Parse("http://127.0.0.1:19000")
	wire := http.DefaultTransport.(*http.Transport).Clone()
	defer wire.CloseIdleConnections()
	var commands, acknowledged atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream := r.Clone(r.Context())
		upstream.URL.Scheme, upstream.URL.Host = target.Scheme, target.Host
		upstream.RequestURI = ""
		// Preserve the signed Host; this proxy only changes the dial target.
		response, err := wire.RoundTrip(upstream)
		if err != nil {
			t.Error(err)
			w.WriteHeader(502)
			return
		}
		defer response.Body.Close()
		if r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/add-user") {
			commands.Add(1)
			_, _ = io.Copy(io.Discard, response.Body)
			if response.StatusCode == 200 {
				acknowledged.Add(1)
			}
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close() // Native MinIO has replied; the API never receives it.
			return
		}
		for name, values := range response.Header {
			w.Header()[name] = values
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	defer proxy.Close()
	connection := runtimesvc.Connection{Endpoint: proxy.URL, AccessKey: "minio-web-test", SecretKey: "minio-web-test-secret", Region: "us-east-1"}
	s := testsession.With(t, connection, testConfig(), nil)
	observer := testsession.With(t, connection, testConfig(), nil)
	app := runtimesvc.New(testConfig())
	defer app.Close()
	if err := app.StoreSession(s); err != nil {
		t.Fatal(err)
	}
	admin, err := storage.NewAdmin(storage.Config{Endpoint: target, Credentials: credentials.NewStaticV4(connection.AccessKey, connection.SecretKey, "")}, wire)
	if err != nil {
		t.Fatal(err)
	}
	name := "lost-reply-" + strings.ToLower(securetoken.New()[:10])
	t.Cleanup(func() {
		if err := admin.RemoveUser(context.Background(), name); err != nil {
			t.Error("test user cleanup failed", err)
		}
	})
	previous := app.AccessRevision(observer)
	payload, _ := json.Marshal(map[string]any{"acknowledgeNonAtomicCreate": true, "name": name, "secretKey": "Uncertain-fixture-secret"})
	r := httptest.NewRequest("POST", "http://ui.example/api/sessions/"+s.SessionID()+"/admin/users", bytes.NewReader(payload))
	r.Header.Set("Origin", "http://ui.example")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", s.CSRFToken())
	r.AddCookie(httpcommon.SessionCookie(s, r, false))
	w := httptest.NewRecorder()
	apihttp.NewHandler(app).ServeHTTP(w, r)
	if w.Code != 502 || commands.Load() != 1 || acknowledged.Load() != 1 || app.AccessRevision(observer) == previous {
		t.Fatal("applied command replayed or revision stayed stale", w.Code, commands.Load(), acknowledged.Load(), previous, app.AccessRevision(observer))
	}
	user, err := admin.GetUserInfo(context.Background(), name)
	if err != nil || user.Status != madmin.AccountEnabled {
		t.Fatal("native mutation not applied", err)
	}
}

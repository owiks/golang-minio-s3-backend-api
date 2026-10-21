package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"minio-web/api/src/config"
	"minio-web/api/src/http/middleware"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/runtime"
	"minio-web/api/tests/testsession"
)

func TestPrivateSessionPreservesAuthenticationAndCSRF(t *testing.T) {
	for _, tc := range []struct {
		name, method, cookie, csrf, origin, code string
		expired, missing                         bool
	}{
		{name: "read", method: "GET", cookie: "valid"},
		{name: "write", method: "PUT", cookie: "valid", csrf: "valid", origin: "http://ui.example"},
		{name: "unknown-session", method: "GET", cookie: "valid", missing: true, code: "SessionExpired"},
		{name: "expired", method: "GET", cookie: "valid", expired: true, code: "SessionExpired"},
		{name: "no-cookie", method: "GET", code: "SessionExpired"},
		{name: "wrong-cookie", method: "GET", cookie: "wrong", code: "SessionExpired"},
		{name: "no-csrf", method: "DELETE", cookie: "valid", origin: "http://ui.example", code: "CSRFDenied"},
		{name: "wrong-csrf", method: "PUT", cookie: "valid", csrf: "wrong", origin: "http://ui.example", code: "CSRFDenied"},
		{name: "no-origin", method: "POST", cookie: "valid", csrf: "valid", code: "CSRFDenied"},
		{name: "foreign-origin", method: "POST", cookie: "valid", csrf: "valid", origin: "https://evil.example", code: "CSRFDenied"},
		{name: "head-still-needs-csrf", method: "HEAD", cookie: "valid", code: "CSRFDenied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Origin = "http://ui.example"
			if tc.expired {
				cfg.SessionTTL = -time.Hour
			}
			app := runtime.New(cfg)
			defer app.Close()
			s := testsession.With(t, runtime.Connection{}, cfg, nil)
			if err := app.StoreSession(s); err != nil {
				t.Fatal(err)
			}
			id := s.SessionID()
			if tc.missing {
				id = "missing"
			}
			r := httptest.NewRequest(tc.method, "http://ui.example/api/sessions/"+id, nil)
			if tc.cookie != "" {
				value := tc.cookie
				if value == "valid" {
					value = s.CookieSnapshot().Token
				}
				r.AddCookie(&http.Cookie{Name: "minio_session_" + id, Value: value})
			}
			csrf := tc.csrf
			if csrf == "valid" {
				csrf = s.CSRFToken()
			}
			r.Header.Set("X-CSRF-Token", csrf)
			r.Header.Set("Origin", tc.origin)
			got, err := middleware.Authenticate(app, r, id)
			if tc.code == "" {
				if err != nil || got != s {
					t.Fatal("valid session rejected", err)
				}
			} else if err == nil || got != nil || apperror.Public(err).Code != tc.code {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
		})
	}
}

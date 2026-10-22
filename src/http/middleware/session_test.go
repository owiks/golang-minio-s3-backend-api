package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/middleware"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
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
				if err != nil || (got == nil || got.SessionID() != s.SessionID()) {
					t.Fatal("valid session rejected", err)
				}
			} else if err == nil || got != nil || apperror.Public(err).Code != tc.code {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
		})
	}
}

func TestBearerAuthenticationIsExclusiveAndRevocable(t *testing.T) {
	cfg := config.Default()
	app := runtime.New(cfg)
	defer app.Close()
	endpoint, _ := url.Parse("http://fixture.example")
	for _, tc := range []struct {
		name                                   string
		header                                 []string
		cookie, expired, revoked, other, query bool
		want                                   bool
	}{
		{name: "read-and-write-without-csrf", header: []string{"Bearer valid"}, want: true},
		{name: "scheme-case-and-spaces", header: []string{"bEaReR  valid"}, want: true},
		{name: "missing"},
		{name: "query-is-not-authentication", query: true},
		{name: "cookie-is-not-authentication", cookie: true},
		{name: "wrong", header: []string{"Bearer wrong"}},
		{name: "basic", header: []string{"Basic valid"}},
		{name: "empty", header: []string{""}},
		{name: "empty-token", header: []string{"Bearer "}},
		{name: "combined", header: []string{"Bearer valid, Bearer valid"}},
		{name: "trailing-space", header: []string{"Bearer valid "}},
		{name: "tab", header: []string{"Bearer\tvalid"}},
		{name: "duplicates", header: []string{"Bearer valid", "Bearer valid"}},
		{name: "expired", header: []string{"Bearer valid"}, expired: true},
		{name: "revoked", header: []string{"Bearer valid"}, revoked: true},
		{name: "other-session", header: []string{"Bearer valid"}, other: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range []string{"GET", "PUT", "DELETE"} {
				tr := http.DefaultTransport.(*http.Transport).Clone()
				defer tr.CloseIdleConnections()
				sessionCfg := cfg
				if tc.expired {
					sessionCfg.SessionTTL = -time.Second
				}
				s, token, err := runtime.NewBearerSession(runtime.Connection{AccessKey: "fixture", SecretKey: "fixture-secret"}, sessionCfg, endpoint, endpoint, tr)
				if err != nil {
					t.Fatal(err)
				}
				if err := app.StoreSession(s); err != nil {
					t.Fatal(err)
				}
				id := s.SessionID()
				if tc.revoked {
					app.RemoveSession(id)
				}
				if tc.other {
					other := testsession.New(t, endpoint.String())
					if err := app.StoreSession(other); err != nil {
						t.Fatal(err)
					}
					id = other.SessionID()
				}
				r := httptest.NewRequest(method, "http://ui/api/sessions/"+id, nil)
				for _, value := range tc.header {
					r.Header.Add("Authorization", strings.ReplaceAll(value, "valid", token))
				}
				if tc.cookie {
					r.AddCookie(&http.Cookie{Name: "minio_session_" + id, Value: token})
				}
				if tc.query {
					r.URL.RawQuery = "access_token=" + token
				}
				got, err := middleware.Authenticate(app, r, id)
				if tc.want {
					if err != nil || (got == nil || got.SessionID() != s.SessionID()) {
						t.Fatal("valid bearer rejected", err)
					}
				} else if err == nil || got != nil || apperror.Public(err).Status != 401 {
					t.Fatal("invalid bearer accepted", err)
				}
			}
		})
	}
}

func TestAuthorizationNeverFallsBackToCookie(t *testing.T) {
	app := runtime.New(config.Default())
	defer app.Close()
	s := testsession.New(t, "http://fixture.example")
	if err := app.StoreSession(s); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"Bearer wrong", "Bearer " + s.CookieSnapshot().Token, "Basic wrong", ""} {
		r := httptest.NewRequest("GET", "http://ui/api/sessions/"+s.SessionID(), nil)
		r.AddCookie(&http.Cookie{Name: "minio_session_" + s.SessionID(), Value: s.CookieSnapshot().Token})
		r.Header.Set("Authorization", value)
		if _, err := middleware.Authenticate(app, r, s.SessionID()); err == nil || apperror.Public(err).Code != "InvalidToken" {
			t.Fatal("cookie fallback accepted", err)
		}
	}
}

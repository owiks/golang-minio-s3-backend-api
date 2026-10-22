package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

func TestTokenIssuanceGuardsAndLocalizedErrors(t *testing.T) {
	for _, tc := range []struct {
		name, origin, body, language, code string
		status                             int
	}{
		{"no-origin", "", `{}`, "en", "InvalidCredentials", 400},
		{"same-origin", "http://ui", `{}`, "en", "InvalidCredentials", 400},
		{"foreign-origin", "https://evil.example", `{}`, "ru", "OriginDenied", 403},
		{"null-origin", "null", `{}`, "en", "OriginDenied", 403},
		{"malformed", "", `{`, "ru", "InvalidJSON", 400},
		{"ttl-zero", "", `{"ttlSeconds":0}`, "en", "InvalidTokenTTL", 400},
		{"ttl-over-limit", "", `{"ttlSeconds":3601}`, "ru", "InvalidTokenTTL", 400},
		{"ttl-negative", "", `{"ttlSeconds":-1}`, "ru", "InvalidTokenTTL", 400},
		{"ttl-fractional", "", `{"ttlSeconds":1.5}`, "en", "InvalidJSON", 400},
		{"unknown-field", "", `{"unknown":true}`, "ru", "InvalidJSON", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := runtime.New(config.Default())
			defer app.Close()
			r := httptest.NewRequest("POST", "http://ui/api/tokens", strings.NewReader(tc.body))
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Accept-Language", tc.language)
			w := httptest.NewRecorder()
			ServeHTTP(app, w, r)
			var body struct {
				Error struct{ Code, Message string }
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status || body.Error.Code != tc.code || body.Error.Message == "" || strings.Contains(body.Error.Message, "http.") || strings.Contains(body.Error.Message, "sessions.") {
				t.Fatal(w.Code, w.Body.String())
			}
			if len(w.Result().Cookies()) != 0 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Language") != tc.language {
				t.Fatal("unsafe token response headers", w.Header())
			}
		})
	}
}

func TestTokenAndCookieLoginShareAttemptBudget(t *testing.T) {
	cfg := config.Default()
	cfg.Limits.LoginBurst = 1
	cfg.Limits.LoginInterval = time.Minute
	app := runtime.New(cfg)
	defer app.Close()
	for _, path := range []string{"/api/tokens", "/api/sessions"} {
		r := httptest.NewRequest("POST", "http://ui"+path, strings.NewReader(`{}`))
		r.Header.Set("Origin", "http://ui")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		ServeHTTP(app, w, r)
		if path == "/api/tokens" && w.Code != 400 || path == "/api/sessions" && (w.Code != 429 || w.Header().Get("Retry-After") == "") {
			t.Fatal("login budgets are not shared", w.Code, w.Body.String())
		}
	}
}

func TestTokenRejectsDuplicateOriginsAndNonJSON(t *testing.T) {
	app := runtime.New(config.Default())
	defer app.Close()
	for _, duplicate := range []bool{false, true} {
		r := httptest.NewRequest("POST", "http://ui/api/tokens", strings.NewReader(`{}`))
		want := 415
		if duplicate {
			r.Header.Add("Origin", "http://ui")
			r.Header.Add("Origin", "https://evil.example")
			r.Header.Set("Content-Type", "application/json")
			want = 403
		}
		w := httptest.NewRecorder()
		ServeHTTP(app, w, r)
		if w.Code != want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestBearerChallengeWithoutCookieFallback(t *testing.T) {
	app := runtime.New(config.Default())
	defer app.Close()
	s := testsession.New(t, "http://fixture.example")
	if err := app.StoreSession(s); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{s.SessionID(), "missing"} {
		r := httptest.NewRequest("GET", "http://ui/api/sessions/"+id+"/access", nil)
		r.Header.Set("Authorization", "Bearer invalid-secret")
		r.Header.Set("Accept-Language", "ru")
		r.AddCookie(&http.Cookie{Name: "minio_session_" + id, Value: s.CookieSnapshot().Token})
		w := httptest.NewRecorder()
		ServeHTTP(app, w, r)
		if w.Code != 401 || w.Header().Get("WWW-Authenticate") != `Bearer realm="minio-s3-api", error="invalid_token"` || strings.Contains(w.Body.String(), "invalid-secret") || !strings.Contains(w.Body.String(), `"InvalidToken"`) {
			t.Fatal(w.Code, w.Header(), w.Body.String())
		}
	}
}

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"minio-web/api/src/config"
	"minio-web/api/src/http/middleware"
	"minio-web/api/src/http/presentation"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/telemetry"
	"minio-web/api/tests/testsession"
)

func TestLocalizedHTTPBoundary(t *testing.T) {
	for _, mode := range []string{"auto", "en", "ru"} {
		app := New(config.Config{Language: mode})
		defer app.Close()
		s := testsession.New(t, "http://fixture.invalid")
		if err := app.StoreSession(s); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			method, path, body, header, code string
			status                           int
		}{
			{"GET", "/api/sessions/expired/access", "", "", "SessionExpired", 401},
			{"GET", "/api/sessions/expired/access", "", "ru-RU, en;q=0.8", "SessionExpired", 401},
			{"POST", "/api/sessions", "{", "ru", "InvalidJSON", 400},
			{"POST", "/api/sessions", `{"endpoint":"not-an-endpoint"}`, "en;q=0.9, ru;q=0.5", "InvalidCredentials", 400},
			{"GET", "/missing", "", "ru;q=bad", "NotFound", 404},
			{"DELETE", "/api/sessions", "", "ru", "MethodNotAllowed", 405},
			{"DELETE", "/api/sessions/" + s.SessionID(), "", "ru", "CSRFDenied", 403},
		} {
			r := httptest.NewRequest(tc.method, "http://ui"+tc.path, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", "http://ui")
			r.Header.Set("Accept-Language", tc.header)
			if strings.Contains(tc.path, s.SessionID()) {
				r.AddCookie(&http.Cookie{Name: "minio_session_" + s.SessionID(), Value: s.CookieSnapshot().Token})
			}
			w := httptest.NewRecorder()
			app.ServeHTTP(w, r)
			var body struct {
				Error presentation.Error `json:"error"`
			}
			err := json.Unmarshal(w.Body.Bytes(), &body)
			want := middleware.ChooseLanguage(mode, tc.header)
			if err != nil || w.Code != tc.status || body.Error.Code != tc.code || body.Error.Message == "" || w.Header().Get("Content-Language") != want || (strings.Contains(w.Header().Get("Vary"), "Accept-Language")) != (mode == "auto") {
				t.Fatalf("%s/%s: %d %s headers=%v err=%v", mode, tc.path, w.Code, w.Body, w.Header(), err)
			}
			if tc.code == "SessionExpired" && (want == "ru") != strings.Contains(body.Error.Message, "Сессия") {
				t.Fatalf("incorrect language %s: %q", want, body.Error.Message)
			}
		}
	}
}
func TestConcurrentLanguageIsolation(t *testing.T) {
	app := New(config.Config{})
	defer app.Close()
	var wg sync.WaitGroup
	for i := 0; i < 60; i++ {
		lang := "en"
		if i%2 == 1 {
			lang = "ru"
		}
		wg.Go(func() {
			r := httptest.NewRequest("GET", "http://ui/api/sessions/expired/access", nil)
			r.Header.Set("Accept-Language", lang)
			w := httptest.NewRecorder()
			app.ServeHTTP(w, r)
			var body struct {
				Error presentation.Error `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Error(err)
				return
			}
			if body.Error.Code != "SessionExpired" || (lang == "ru") != strings.Contains(body.Error.Message, "Сессия") || w.Header().Get("Content-Language") != lang {
				t.Errorf("mixed language: %s %s", lang, w.Body)
			}
		})
	}
	wg.Wait()
}
func TestPanicUsesRequestLanguage(t *testing.T) {
	for _, lang := range []string{"en", "ru"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Language", lang)
		r = middleware.LocalizeRequest(r, "auto", nil)
		w := httptest.NewRecorder()
		middleware.Observe(w, r, "panic", &telemetry.Metrics{}, func(http.ResponseWriter, *http.Request) { panic("PRIVATE-PANIC") })
		var body struct {
			Error presentation.Error `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != 500 || body.Error.Code != "InternalError" || body.Error.Message == string(messages.InternalError) || strings.Contains(w.Body.String(), "PRIVATE") || w.Header().Get("Content-Language") != lang {
			t.Fatal(w.Code, w.Body, w.Header())
		}
	}
}

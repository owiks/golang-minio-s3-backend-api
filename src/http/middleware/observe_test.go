package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"minio-web/api/src/http/httperrors"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/telemetry"
)

func TestObserveStreamingAndSafeErrors(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(old)
	r := httptest.NewRequest("GET", "http://ui/api/receive?token=PRIVATE-SIGNED-TOKEN", nil)
	r.Header.Set("Cookie", "PRIVATE-COOKIE")
	r.Header.Set("X-Request-ID", "FORGED-ID")
	w := httptest.NewRecorder()
	var metrics telemetry.Metrics
	Observe(w, r, "GET /api/receive", &metrics, func(w http.ResponseWriter, r *http.Request) {
		telemetry.Identify(r.Context(), "PRIVATE-ACCESS-KEY", "PRIVATE-ENDPOINT")
		httperrors.Respond(w, r, apperror.New(409, "Changed", messages.Literal("Безопасное сообщение")))
	})
	if w.Code != 409 || len(w.Header().Get("X-Request-ID")) != 32 || w.Header().Get("X-Request-ID") == "FORGED-ID" {
		t.Fatal("bad observed response", w)
	}
	for _, secret := range []string{"PRIVATE-", "FORGED-ID"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("private data entered logs")
		}
	}
	if !strings.Contains(logs.String(), "Changed") || !strings.Contains(logs.String(), w.Header().Get("X-Request-ID")) {
		t.Fatal("missing safe error/correlation")
	}
	w = httptest.NewRecorder()
	Observe(w, r, "GET stream", &metrics, func(w http.ResponseWriter, r *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("data"))
	})
	if !w.Flushed || w.Body.String() != "data" {
		t.Fatal("streaming lost")
	}
}
func TestObservePanicBoundary(t *testing.T) {
	for _, test := range []struct {
		name             string
		stream, sentinel bool
	}{{"before headers", false, false}, {"after headers", true, false}, {"abort sentinel", false, true}, {"after error headers", true, false}} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "http://ui/test", nil)
			var m telemetry.Metrics
			func() {
				defer func() {
					p := recover()
					if test.stream || test.sentinel {
						if p != http.ErrAbortHandler {
							t.Fatalf("stream must abort: %v", p)
						}
					} else if p != nil {
						t.Fatal(p)
					}
				}()
				Observe(w, r, "test", &m, func(w http.ResponseWriter, r *http.Request) {
					if test.stream {
						status := 200
						if test.name == "after error headers" {
							status = 500
						}
						w.WriteHeader(status)
						_, _ = w.Write([]byte("partial"))
					}
					if test.sentinel {
						panic(http.ErrAbortHandler)
					}
					panic("PRIVATE-PANIC")
				})
			}()
			if strings.Contains(w.Body.String(), "PRIVATE-PANIC") {
				t.Fatal("panic leaked")
			}
			if test.stream && w.Body.String() != "partial" {
				t.Fatal("error appended to stream")
			}
			if !test.stream && !test.sentinel && w.Code != 500 {
				t.Fatal(w.Code)
			}
		})
	}
}

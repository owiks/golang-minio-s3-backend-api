package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minio/madmin-go/v3"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/middleware"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/server"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
)

type upstreamTestWire func(*http.Request) (*http.Response, error)

func (f upstreamTestWire) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestObservedHTTPErrorKeepsPrivateDiagnosticsOutsidePublicJSON(t *testing.T) {
	for _, scenario := range []string{"DNS", "SDK", "opaque mapped cause", "local after read"} {
		t.Run(scenario, func(t *testing.T) {
			app := runtime.New(config.Config{})
			defer app.Close()
			var logs bytes.Buffer
			old := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			defer slog.SetDefault(old)
			wantClass, wantStage, wantStatus, wantCode := "", "", 502, "ServerOperationFailed"
			switch scenario {
			case "DNS":
				wantClass, wantStage = "dns", "request"
			case "SDK":
				wantClass, wantStage = "sdk", "operation"
			case "opaque mapped cause":
				wantClass, wantStage = "unknown", "operation"
			case "local after read":
				wantStatus, wantCode = 409, "ServerConfigChanged"
			}
			match := routes.Match{Found: true, MethodAllowed: true, Route: routes.APIRoute{Public: true}, Operation: routes.Operation{Handler: func(_ *runtime.State, _ http.ResponseWriter, r *http.Request, _ *runtime.Session, _ string) error {
				if scenario == "DNS" {
					req, _ := http.NewRequestWithContext(r.Context(), "GET", "https://PRIVATE-HOST/?token=PRIVATE-SIGNATURE", nil)
					response, err := (telemetry.Transport{Base: upstreamTestWire(func(*http.Request) (*http.Response, error) {
						return nil, &net.DNSError{Name: "PRIVATE-HOST", Err: "PRIVATE-DETAIL"}
					})}).RoundTrip(req)
					if response != nil {
						_ = response.Body.Close()
					}
					return server.ServerOperationError(err)
				}
				if scenario == "SDK" {
					return server.ServerOperationError(madmin.ErrorResponse{Code: "PRIVATE-CODE", Message: "PRIVATE-DETAIL"})
				}
				if scenario == "local after read" {
					req, _ := http.NewRequestWithContext(r.Context(), "GET", "http://PRIVATE", nil)
					resp, _ := (telemetry.Transport{Base: upstreamTestWire(func(*http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("data"))}, nil
					})}).RoundTrip(req)
					_ = resp.Body.Close()
					return apperror.New(409, "ServerConfigChanged", messages.Literal("safe"))
				}
				return server.ServerOperationError(errors.New("PRIVATE-OPAQUE-CAUSE"))
			}}}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/test", nil)
			middleware.Observe(w, r, "GET static-test", &telemetry.Metrics{}, func(w http.ResponseWriter, r *http.Request) { serveOperation(app, w, r, match) })
			var record map[string]any
			if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
				t.Fatal(err, logs.String())
			}
			if w.Code != wantStatus || !strings.Contains(w.Body.String(), `"code":"`+wantCode+`"`) || w.Header().Get("X-Request-ID") == "" || record["upstream_failure_class"] != wantClass || record["upstream_failure_stage"] != wantStage {
				t.Fatalf("HTTP/log contract: status=%d body=%s log=%s", w.Code, w.Body.String(), logs.String())
			}
			if strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(logs.String(), "PRIVATE") || strings.Contains(w.Body.String(), "failure_class") || strings.Contains(w.Body.String(), "cause") {
				t.Fatal("sensitive diagnostics leaked")
			}
		})
	}
}

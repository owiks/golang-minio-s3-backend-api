package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/storage"
)

type boundedDiagnosticClient struct {
	cfg       storage.Config
	transport http.RoundTripper
}

func (c boundedDiagnosticClient) OpenDiagnosticTrace(ctx context.Context, onlyErrors bool) (storage.DiagnosticStream[madmin.ServiceTraceInfo], error) {
	return storage.OpenDiagnosticTrace(ctx, c.cfg, c.transport, onlyErrors)
}
func (c boundedDiagnosticClient) OpenDiagnosticLogs(ctx context.Context, limit int) (storage.DiagnosticStream[madmin.LogInfo], error) {
	return storage.OpenDiagnosticLogs(ctx, c.cfg, c.transport, limit)
}

func TestReadDiagnosticsPropagatesRecordLimitAndKeepsNormalInterval(t *testing.T) {
	for _, kind := range []string{"logs", "trace"} {
		for _, oversized := range []bool{false, true} {
			name := kind + "/interval"
			if oversized {
				name = kind + "/limit"
			}
			t.Run(name, func(t *testing.T) {
				stopped := make(chan struct{})
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					defer close(stopped)
					w.Header().Set("Content-Type", "application/json")
					if oversized {
						field := "ConsoleMsg"
						if kind == "trace" {
							field = "msg"
						}
						_, _ = w.Write([]byte(`{"` + field + `":"` + strings.Repeat("x", 2<<20) + `"}` + "\n"))
					} else if kind == "trace" {
						_, _ = w.Write([]byte("{\"funcname\":\"s3.GetObject\",\"traceType\":1}\n"))
					} else {
						_, _ = w.Write([]byte("{\"level\":\"ERROR\",\"node\":\"minio-1\"}\n"))
					}
					_ = http.NewResponseController(w).Flush()
					<-r.Context().Done()
				}))
				defer upstream.Close()
				endpoint, _ := url.Parse(upstream.URL)
				transport := http.DefaultTransport.(*http.Transport).Clone()
				defer transport.CloseIdleConnections()
				client := boundedDiagnosticClient{storage.Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4("key", "secret", "")}, transport}
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				defer cancel()
				report, err := ReadDiagnostics(ctx, client, DiagnosticOptions{Kind: kind, Level: "ALL", Seconds: 1, Limit: 200})
				if oversized {
					if apperror.Public(err).Code != "DiagnosticRecordTooLarge" {
						t.Fatal("SDK limit became empty success or generic close", report, err)
					}
				} else {
					if err != nil || len(report.Rows) != 1 || report.Ended.IsZero() {
						t.Fatal("normal interval became a read error", report, err)
					}
				}
				select {
				case <-stopped:
				case <-ctx.Done():
					t.Fatal("diagnostic HTTP body not cancelled")
				}
			})
		}
	}
}

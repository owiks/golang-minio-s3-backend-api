package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

func diagnosticTestClients(t *testing.T, handler http.HandlerFunc) *Clients {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	endpoint, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(transport.CloseIdleConnections)
	return NewClients(&Session{internal: endpoint, credentials: credentials.NewStaticV4("fixture-key", "fixture-secret", ""), transport: transport})
}

func TestDiagnosticAdaptersKeepNativeScopeAndCloseUpstream(t *testing.T) {
	for _, kind := range []string{"trace", "logs"} {
		t.Run(kind, func(t *testing.T) {
			stopped := make(chan struct{})
			client := diagnosticTestClients(t, func(w http.ResponseWriter, r *http.Request) {
				defer close(stopped)
				if !strings.HasSuffix(r.URL.Path, "/"+map[string]string{"trace": "trace", "logs": "log"}[kind]) {
					t.Error("wrong native endpoint", r.URL.Path)
				}
				query := r.URL.Query()
				if kind == "trace" {
					if query.Get("s3") != "true" || query.Get("err") != "true" || query.Get("internal") != "false" {
						t.Error("native trace scope changed", query)
					}
					json.NewEncoder(w).Encode(madmin.TraceInfo{TraceType: madmin.TraceS3, FuncName: "s3.GetObject"})
				} else {
					if query.Get("limit") != "7" || query.Get("logType") != "ALL" || query.Get("node") != "" {
						t.Error("native log subscription changed", query)
					}
					w.Write([]byte("{\"level\":\"ERROR\",\"node\":\"fixture\",\"api\":{\"name\":\"GetObject\"}}\n"))
				}
				http.NewResponseController(w).Flush()
				<-r.Context().Done()
			})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if kind == "trace" {
				stream, err := client.OpenDiagnosticTrace(ctx, true)
				if err != nil {
					t.Fatal(err)
				}
				defer stream.Close()
				select {
				case v := <-stream.Events():
					if v.Err != nil || v.Trace.FuncName != "s3.GetObject" {
						t.Fatal(v)
					}
				case <-ctx.Done():
					t.Fatal("trace not received")
				}
				if stream.Status() != 200 {
					t.Fatal(stream.Status())
				}
				stream.Close()
			} else {
				stream, err := client.OpenDiagnosticLogs(ctx, 7)
				if err != nil {
					t.Fatal(err)
				}
				defer stream.Close()
				select {
				case v := <-stream.Events():
					if v.Err != nil || v.API == nil || v.API.Name != "GetObject" {
						t.Fatal(v)
					}
				case <-ctx.Done():
					t.Fatal("log not received")
				}
				if stream.Status() != 200 {
					t.Fatal(stream.Status())
				}
				stream.Close()
			}
			select {
			case <-stopped:
			case <-ctx.Done():
				t.Fatal("Close did not interrupt upstream subscription")
			}
		})
	}
}

func TestDiagnosticAdaptersRetainHTTPStatusOnSDKFailure(t *testing.T) {
	for _, kind := range []string{"trace", "logs"} {
		t.Run(kind, func(t *testing.T) {
			client := diagnosticTestClients(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(403)
				w.Write([]byte(`{"Code":"AccessDenied","Message":"PRIVATE-upstream"}`))
			})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if kind == "trace" {
				stream, err := client.OpenDiagnosticTrace(ctx, false)
				if err != nil {
					t.Fatal(err)
				}
				awaitDiagnosticFailure(t, ctx, stream)
			} else {
				stream, err := client.OpenDiagnosticLogs(ctx, 7)
				if err != nil {
					t.Fatal(err)
				}
				awaitDiagnosticFailure(t, ctx, stream)
			}
		})
	}
}
func awaitDiagnosticFailure[T any](t *testing.T, ctx context.Context, stream storage.DiagnosticStream[T]) {
	t.Helper()
	defer stream.Close()
	select {
	case <-stream.Events():
	case <-ctx.Done():
		t.Fatal("SDK failure did not arrive")
	}
	if stream.Status() != 403 {
		t.Fatal("upstream status was lost", stream.Status())
	}
}

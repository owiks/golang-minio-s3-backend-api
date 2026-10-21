package remotesource

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/result"
	"minio-web/api/src/services/runtime"
	"minio-web/api/src/services/securetoken"
	"minio-web/api/src/services/staging"
	"minio-web/api/src/services/uploads"
	"minio-web/api/tests/testsession"
)

func TestHTTPSourceImportNativeIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("requires disposable docker-compose.test.yml fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	native := runtime.NewClients(testsession.Native(t))
	bucket := "http-source-" + strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:10]))
	if err := native.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		for _, key := range []string{"imported.json", "raced.json"} {
			_ = native.RemoveObject(cleanup, bucket, key, minio.RemoveObjectOptions{})
		}
		if err := native.RemoveBucket(cleanup, bucket); err != nil {
			t.Error("fixture cleanup failed", err)
		}
	}()
	const content = `{"name":"Файл","size":42}`
	var requests atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("S3 credentials leaked to remote source")
		}
		if r.URL.Path == "/raced.json" {
			if _, err := native.PutObject(r.Context(), bucket, "raced.json", strings.NewReader("external"), 8, minio.PutObjectOptions{}); err != nil {
				t.Error(err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		io.WriteString(w, content)
	}))
	defer upstream.Close()
	wire := upstream.Client()
	wire.Transport.(*http.Transport).DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
	}
	client := &Client{http: wire}
	defer client.Close()
	for _, key := range []string{"imported.json", "raced.json"} {
		t.Run(key, func(t *testing.T) {
			job, err := uploads.PrepareImport(ctx, native, bucket, key, uploads.ImportRequest{URL: "https://127.0.0.1/" + key, Revision: "absent"})
			if err != nil {
				t.Fatal(err)
			}
			budget := staging.New(1024)
			var progress []result.Progress
			size, err := uploads.Import(ctx, budget, client, job, func(event result.StreamEvent[uploads.ImportResult]) {
				if event.Progress != nil {
					progress = append(progress, *event.Progress)
				}
			})
			if budget.Used() != 0 || size != int64(len(content)) || len(progress) < 2 {
				t.Fatal("budget, byte count or progress changed", err)
			}
			if key == "raced.json" {
				if err == nil || apperror.Public(err).Code != "DestinationChanged" {
					t.Fatal("concurrent destination was overwritten", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			object, err := native.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer object.Close()
			body, err := io.ReadAll(object)
			if err != nil {
				t.Fatal(err)
			}
			want := content
			if key == "raced.json" {
				want = "external"
			}
			if string(body) != want {
				t.Fatal("unexpected native object contents")
			}
			if key == "imported.json" {
				info, err := native.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
				if err != nil || info.ContentType != "application/json" {
					t.Fatal("source metadata lost", err)
				}
			}
		})
	}
	if requests.Load() != 2 {
		t.Fatal("source request repeated", requests.Load())
	}
}

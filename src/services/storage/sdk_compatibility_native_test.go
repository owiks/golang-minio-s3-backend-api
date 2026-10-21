package storage_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/objects/write"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

// Only the fixed disposable docker-compose.test.yml fixture is accepted. The racing
// writer uses its own SDK client and therefore bypasses all application locks.
func TestSDKConditionalMultipartNativeIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("set MINIO_INTEGRATION=1 with the isolated docker-compose.test.yml fixture")
	}
	endpoint, _ := url.Parse("http://127.0.0.1:19000")
	cfg := storage.Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", ""), Region: "us-east-1"}
	raw, err := storage.NewS3(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	bucket := fmt.Sprintf("sdk-conditional-%x", time.Now().UnixNano())
	if err := raw.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		for _, key := range []string{"create.txt", "overwrite.txt"} {
			_ = raw.RemoveObject(cleanup, bucket, key, minio.RemoveObjectOptions{})
		}
		if err := raw.RemoveBucket(cleanup, bucket); err != nil {
			t.Error("isolated bucket cleanup failed", err)
		}
	})
	for _, overwrite := range []bool{false, true} {
		t.Run(fmt.Sprintf("overwrite=%t", overwrite), func(t *testing.T) {
			key := "create.txt"
			current := objectwrite.FileState{Revision: "checked"}
			if overwrite {
				key = "overwrite.txt"
				info, err := raw.PutObject(ctx, bucket, key, strings.NewReader("before"), 6, minio.PutObjectOptions{})
				if err != nil {
					t.Fatal(err)
				}
				current.Exists, current.ETag = true, info.ETag
			}
			var completes, aborts atomic.Int32
			wire := compatibilityTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodPost && r.URL.Query().Has("uploadId") {
					completes.Add(1)
					if overwrite && r.Header.Get("If-Match") == "" || !overwrite && r.Header.Get("If-None-Match") != "*" {
						t.Error("native destination condition absent")
					}
					if _, err := raw.PutObject(ctx, bucket, key, strings.NewReader("external writer"), 15, minio.PutObjectOptions{}); err != nil {
						t.Error("isolated racing write failed", err)
						return nil, err
					}
				}
				if r.Method == http.MethodDelete && r.URL.Query().Has("uploadId") {
					aborts.Add(1)
				}
				return http.DefaultTransport.RoundTrip(r)
			})
			native, err := storage.NewS3(cfg, wire)
			if err != nil {
				t.Fatal(err)
			}
			opts, err := objectwrite.WriteOptions(current, "checked", "application/octet-stream")
			if err != nil {
				t.Fatal(err)
			}
			size := int64(opts.PartSize) + 1
			_, err = storage.NewS3Client(native).PutObject(ctx, bucket, key, io.LimitReader(compatibilityZeroReader{}, size), size, opts)
			code := minio.ToErrorResponse(err).Code
			if err == nil || code != "PreconditionFailed" && code != "ConditionalRequestConflict" || completes.Load() != 1 || aborts.Load() != 1 {
				t.Fatal("racing destination was overwritten or commit repeated", code, completes.Load(), aborts.Load(), err)
			}
			object, err := raw.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(object)
			_ = object.Close()
			if err != nil || string(data) != "external writer" {
				t.Fatal("external writer's object changed", string(data), err)
			}
		})
	}
}

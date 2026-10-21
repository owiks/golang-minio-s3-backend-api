package transfers

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

type copyTransport func(*http.Request) (*http.Response, error)

func (f copyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestServerCopyNativeConditions(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("isolated fixture")
	}
	ctx := context.Background()
	creds := credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", "")
	plain, err := minio.New("127.0.0.1:19000", &minio.Options{Creds: creds, Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	bucket := "copy-" + strings.ToLower(securetoken.New()[:10])
	bucket = strings.NewReplacer("_", "a", "-", "b").Replace(bucket)
	if err = plain.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for obj := range plain.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true, WithVersions: true}) {
			_ = plain.RemoveObject(ctx, bucket, obj.Key, minio.RemoveObjectOptions{VersionID: obj.VersionID})
		}
		_ = plain.RemoveBucket(ctx, bucket)
	})
	for _, scenario := range []string{"normal", "destination-created", "destination-overwritten", "source-changed"} {
		t.Run(scenario, func(t *testing.T) {
			source := "source-" + scenario
			dest := "dest-" + scenario
			put := func(key, body string) {
				if _, e := plain.PutObject(ctx, bucket, key, strings.NewReader(body), int64(len(body)), minio.PutObjectOptions{}); e != nil {
					t.Fatal(e)
				}
			}
			put(source, "before")
			info, e := plain.StatObject(ctx, bucket, source, minio.StatObjectOptions{})
			if e != nil {
				t.Fatal(e)
			}
			opts := minio.PutObjectOptions{UserMetadata: map[string]string{"project": "qa"}, UserTags: map[string]string{"team": "qa"}, ContentType: "text/plain"}
			if scenario != "destination-overwritten" {
				opts.SetMatchETagExcept("*")
			}
			if scenario == "destination-overwritten" {
				put(dest, "old")
				old, _ := plain.StatObject(ctx, bucket, dest, minio.StatObjectOptions{})
				opts.SetMatchETag(old.ETag)
			}
			gets, parts, completes, aborts := 0, 0, 0, 0
			uploadID, abortStatus := "", 0
			changed := false
			transport := copyTransport(func(r *http.Request) (*http.Response, error) {
				if id := r.URL.Query().Get("uploadId"); id != "" {
					uploadID = id
				}
				if r.Method == "GET" {
					gets++
				}
				if r.Method == "PUT" && r.URL.Query().Has("partNumber") {
					parts++
					if r.Header.Get("X-Amz-Copy-Source-If-Match") != `"`+info.ETag+`"` {
						t.Error("source condition missing")
					}
					if scenario == "source-changed" && !changed {
						changed = true
						put(source, "new-source")
					}
				}
				if r.Method == "POST" && r.URL.Query().Has("uploadId") {
					completes++
					if scenario == "destination-overwritten" {
						if r.Header.Get("If-Match") == "" {
							t.Error("overwrite condition missing")
						}
					} else if r.Header.Get("If-None-Match") != "*" {
						t.Error("absence condition missing")
					}
					if strings.HasPrefix(scenario, "destination-") && !changed {
						changed = true
						put(dest, "external")
					}
				}
				response, err := http.DefaultTransport.RoundTrip(r)
				if r.Method == http.MethodDelete && r.URL.Query().Has("uploadId") {
					aborts++
					if response != nil {
						abortStatus = response.StatusCode
					}
				}
				return response, err
			})
			client, e := minio.New("127.0.0.1:19000", &minio.Options{Creds: creds, Region: "us-east-1", Transport: transport, MaxRetries: 1})
			if e != nil {
				t.Fatal(e)
			}
			_, handled, e := copyOnServer(ctx, storage.NewS3Client(client), FileTransferRequest{SourceBucket: bucket, SourceKey: source, DestinationBucket: bucket, DestinationKey: dest}, info, opts)
			if !handled || gets != 0 || parts != 1 {
				t.Fatal("copy streamed source instead of server part", handled, gets, parts, e)
			}
			if scenario == "normal" {
				if e != nil || completes != 1 {
					t.Fatal(e)
				}
				obj, _ := plain.GetObject(ctx, bucket, dest, minio.GetObjectOptions{})
				data, _ := io.ReadAll(obj)
				obj.Close()
				if string(data) != "before" {
					t.Fatal("body not copied")
				}
				meta, _ := plain.StatObject(ctx, bucket, dest, minio.StatObjectOptions{})
				tags, _ := plain.GetObjectTagging(ctx, bucket, dest, minio.GetObjectTaggingOptions{})
				if meta.UserMetadata["Project"] != "qa" || tags.ToMap()["team"] != "qa" {
					t.Fatal("metadata lost")
				}
			} else if e == nil {
				t.Fatal("racing write overwritten")
			}
			if strings.HasPrefix(scenario, "destination-") {
				obj, _ := plain.GetObject(ctx, bucket, dest, minio.GetObjectOptions{})
				data, _ := io.ReadAll(obj)
				obj.Close()
				if string(data) != "external" {
					t.Fatal("external destination lost")
				}
			}
			wantAborts := 1
			if scenario == "normal" {
				wantAborts = 0
			}
			if uploadID == "" || aborts != wantAborts {
				t.Fatal("multipart cleanup was not attempted exactly once", uploadID, aborts, wantAborts)
			}
			core := minio.Core{Client: plain}
			// An empty-prefix listing uses MinIO's node-local multipart cache.
			// Check the exact upload and key against storage instead.
			_, partsErr := core.ListObjectParts(ctx, bucket, dest, uploadID, 0, 1)
			if minio.ToErrorResponse(partsErr).Code != "NoSuchUpload" {
				t.Fatalf("copy upload still exists: key=%s upload=%s abort_status=%d error=%v", dest, uploadID, abortStatus, partsErr)
			}
			pending, e := core.ListMultipartUploads(ctx, bucket, dest, "", "", "", 100)
			if e != nil || len(pending.Uploads) != 0 {
				t.Fatalf("multipart leaked: key=%s uploads=%+v abort_status=%d error=%v", dest, pending.Uploads, abortStatus, e)
			}
		})
	}
}

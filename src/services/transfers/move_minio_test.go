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

	"minio-web/api/src/services/objects/state"
	"minio-web/api/src/services/securetoken"
	"minio-web/api/src/services/storage"
)

type minioMoveClient struct{ *storage.S3Client }

func (minioMoveClient) ConditionalDeleteSupported() bool { return false }
func (minioMoveClient) RemoveConditional(context.Context, string, string, string) error {
	panic("MinIO move must use the verified ordinary DELETE path")
}

// Run against the isolated MinIO fixture, including real deletes. Transport
// hooks introduce competing writes and uncertain responses at protocol boundaries.
func TestMoveNativeMinIO(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("isolated fixture")
	}
	ctx := context.Background()
	creds := credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", "")
	plain, err := minio.New("127.0.0.1:19000", &minio.Options{Creds: creds, Region: "us-east-1", MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"normal", "versioned", "source-changed", "destination-changed", "delete-retained", "source-recreated", "verification-denied"} {
		t.Run(scenario, func(t *testing.T) {
			bucket := "move-" + strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:10]))
			if err := plain.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				for obj := range plain.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true, WithVersions: true}) {
					_ = plain.RemoveObject(ctx, bucket, obj.Key, minio.RemoveObjectOptions{VersionID: obj.VersionID})
				}
				_ = plain.RemoveBucket(ctx, bucket)
			})
			if scenario == "versioned" {
				if err := plain.EnableVersioning(ctx, bucket); err != nil {
					t.Fatal(err)
				}
			}
			put := func(key, body string) {
				if _, err := plain.PutObject(ctx, bucket, key, strings.NewReader(body), int64(len(body)), minio.PutObjectOptions{ContentType: "text/plain", UserMetadata: map[string]string{"project": "qa"}, UserTags: map[string]string{"team": "qa"}}); err != nil {
					t.Fatal(err)
				}
			}
			put("source", "original")
			before, err := plain.StatObject(ctx, bucket, "source", minio.StatObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			deletes := 0
			transport := copyTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method == "HEAD" && r.URL.Path == "/"+bucket+"/source" && deletes > 0 && scenario == "verification-denied" {
					return &http.Response{StatusCode: 403, Status: "403 Forbidden", Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				}
				if r.Method == "DELETE" && r.URL.Path == "/"+bucket+"/source" {
					deletes++
					if r.URL.Query().Has("versionId") || r.Header.Get("X-Amz-Bypass-Governance-Retention") != "" {
						t.Error("move must not permanently delete versions or bypass retention")
					}
					if scenario == "delete-retained" {
						return &http.Response{StatusCode: 204, Status: "204 No Content", Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
					}
				}
				response, err := http.DefaultTransport.RoundTrip(r)
				if err == nil && response.StatusCode < 300 {
					if r.Method == "POST" && r.URL.Query().Has("uploadId") {
						if scenario == "source-changed" {
							put("source", "external source")
						}
						if scenario == "destination-changed" {
							put("destination", "external destination")
						}
					}
					if r.Method == "DELETE" && r.URL.Path == "/"+bucket+"/source" && scenario == "source-recreated" {
						put("source", "recreated")
					}
				}
				return response, err
			})
			client, err := minio.New("127.0.0.1:19000", &minio.Options{Creds: creds, Region: "us-east-1", MaxRetries: 1, Transport: transport})
			if err != nil {
				t.Fatal(err)
			}
			revision := objectstate.DeletionRevision(objectstate.DeletionSnapshot{ETag: before.ETag, Size: before.Size, Modified: before.LastModified, Version: before.VersionID})
			result, err := Transfer(ctx, minioMoveClient{storage.NewS3Client(client)}, FileTransferRequest{SourceBucket: bucket, SourceKey: "source", SourceRevision: revision, DestinationBucket: bucket, DestinationKey: "destination", DestinationRevision: "absent", Move: true})
			if err != nil || !result.Copied {
				t.Fatal(result, err)
			}
			expected := map[string]string{"source-changed": "SourceChanged", "destination-changed": "DestinationChanged", "delete-retained": "SourceStillExists", "source-recreated": "SourceStillExists", "verification-denied": "DeleteUncertain"}[scenario]
			if result.Code != expected || result.Moved != (expected == "") {
				t.Fatalf("unexpected outcome: %+v, expected code %q", result, expected)
			}
			wantDeletes := 1
			if scenario == "source-changed" || scenario == "destination-changed" {
				wantDeletes = 0
			}
			if deletes != wantDeletes {
				t.Fatalf("DELETE calls %d, want %d", deletes, wantDeletes)
			}
			read := func(key, version string) string {
				obj, err := plain.GetObject(ctx, bucket, key, minio.GetObjectOptions{VersionID: version})
				if err != nil {
					t.Fatal(err)
				}
				defer obj.Close()
				data, err := io.ReadAll(obj)
				if err != nil {
					t.Fatal(err)
				}
				return string(data)
			}
			wantBody := "original"
			if scenario == "destination-changed" {
				wantBody = "external destination"
			}
			if body := read("destination", ""); body != wantBody {
				t.Fatal(body)
			}
			if result.Moved {
				if _, err := plain.StatObject(ctx, bucket, "source", minio.StatObjectOptions{}); minio.ToErrorResponse(err).Code != "NoSuchKey" {
					t.Fatal("source remains", err)
				}
			}
			if scenario == "source-changed" && read("source", "") != "external source" {
				t.Fatal("competing source lost")
			}
			if scenario == "source-recreated" && read("source", "") != "recreated" {
				t.Fatal("recreated source lost")
			}
			if scenario == "versioned" && read("source", before.VersionID) != "original" {
				t.Fatal("prior version lost")
			}
			meta, err := plain.StatObject(ctx, bucket, "destination", minio.StatObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			tags, err := plain.GetObjectTagging(ctx, bucket, "destination", minio.GetObjectTaggingOptions{})
			if err != nil || meta.UserMetadata["Project"] != "qa" || tags.ToMap()["team"] != "qa" {
				t.Fatal("metadata/tags lost", meta, tags, err)
			}
		})
	}
}

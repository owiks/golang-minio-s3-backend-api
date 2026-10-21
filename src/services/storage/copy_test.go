package storage

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestCopyFallbackRequiresExplicitUnsupportedAndCleanup(t *testing.T) {
	for _, scenario := range []string{"unsupported-init", "unsupported-part", "denied-part", "embedded-error", "unsupported-completion", "abort-failed"} {
		t.Run(scenario, func(t *testing.T) {
			aborts, gets, completes := 0, 0, 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				failure := func(status int, code string) {
					w.WriteHeader(status)
					fmt.Fprintf(w, "<Error><Code>%s</Code></Error>", code)
				}
				switch {
				case r.Method == "GET":
					gets++
					t.Error("unexpected object download")
				case r.Method == "POST" && r.URL.Query().Has("uploads"):
					if scenario == "unsupported-init" {
						failure(501, "NotImplemented")
						return
					}
					fmt.Fprint(w, "<InitiateMultipartUploadResult><UploadId>test-upload</UploadId></InitiateMultipartUploadResult>")
				case r.Method == "PUT":
					switch scenario {
					case "unsupported-part", "abort-failed":
						failure(501, "NotImplemented")
					case "denied-part":
						failure(403, "AccessDenied")
					case "embedded-error":
						failure(200, "AccessDenied")
					default:
						fmt.Fprint(w, "<CopyPartResult><ETag>part-etag</ETag></CopyPartResult>")
					}
				case r.Method == "POST":
					completes++
					failure(501, "NotImplemented")
				case r.Method == "DELETE":
					aborts++
					if scenario == "abort-failed" {
						failure(403, "AccessDenied")
					} else {
						w.WriteHeader(204)
					}
				default:
					t.Error("unexpected copy request")
				}
			}))
			defer upstream.Close()
			client, e := minio.New(strings.TrimPrefix(upstream.URL, "http://"), &minio.Options{Creds: credentials.NewStaticV4("key", "secret", ""), Region: "us-east-1", BucketLookup: minio.BucketLookupPath, MaxRetries: 1})
			if e != nil {
				t.Fatal(e)
			}
			opts := minio.PutObjectOptions{}
			opts.SetMatchETagExcept("*")
			_, handled, e := CopyConditional(context.Background(), NewS3Client(client), CopyRequest{SourceBucket: "source-bucket", SourceKey: "file", DestinationBucket: "target-bucket", DestinationKey: "copy"}, minio.ObjectInfo{Size: 6, ETag: "source-etag"}, opts, 64*1024*1024)
			fallback := scenario == "unsupported-init" || scenario == "unsupported-part"
			if handled == fallback || (e == nil) != fallback || gets != 0 {
				t.Fatal("unsafe fallback", handled, e, gets)
			}
			if scenario != "unsupported-init" && aborts == 0 {
				t.Fatal("upload not cleaned")
			}
			if scenario == "unsupported-completion" && completes != 1 {
				t.Fatal("completion not checked")
			}
		})
	}
}

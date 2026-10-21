package storage

import (
	"context"
	"fmt"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmptyCopyChecksSourceBeforeDestinationWrite(t *testing.T) {
	for _, tc := range []struct {
		name, code, body string
		status           int
		write            bool
	}{
		{name: "empty", status: 200, write: true},
		{name: "missing", status: 404, code: "NoSuchKey"},
		{name: "missing-version", status: 404, code: "NoSuchVersion"},
		{name: "changed-etag", status: 412, code: "PreconditionFailed"},
		{name: "denied", status: 403, code: "AccessDenied"},
		{name: "became-nonempty", status: 200, body: "x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gets, puts int
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case "GET":
					gets++
					if r.URL.Path != "/source-bucket/file" || r.URL.Query().Get("versionId") != "source-version" || r.Header.Get("If-Match") != `"source-etag"` {
						t.Errorf("source condition lost: %s %s", r.URL, r.Header.Get("If-Match"))
					}
					w.Header().Set("ETag", `"source-etag"`)
					w.Header().Set("Last-Modified", "Tue, 14 Nov 2023 22:13:20 GMT")
					w.WriteHeader(tc.status)
					if tc.code != "" {
						fmt.Fprintf(w, "<Error><Code>%s</Code></Error>", tc.code)
					} else {
						fmt.Fprint(w, tc.body)
					}
				case "PUT":
					puts++
					if !tc.write || gets != 1 || r.URL.Path != "/target-bucket/copy" || r.Header.Get("If-None-Match") != "*" {
						t.Errorf("unsafe destination write: %s", r.URL)
					}
					w.Header().Set("ETag", `"empty-etag"`)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					w.WriteHeader(500)
				}
			}))
			defer upstream.Close()
			client, err := minio.New(strings.TrimPrefix(upstream.URL, "http://"), &minio.Options{Region: "us-east-1", Creds: credentials.NewStaticV4("key", "secret", ""), BucketLookup: minio.BucketLookupPath, MaxRetries: 1})
			if err != nil {
				t.Fatal(err)
			}
			opts := minio.PutObjectOptions{}
			opts.SetMatchETagExcept("*")
			_, handled, err := CopyConditional(context.Background(), NewS3Client(client), CopyRequest{SourceBucket: "source-bucket", SourceKey: "file", DestinationBucket: "target-bucket", DestinationKey: "copy"}, minio.ObjectInfo{Size: 0, ETag: "source-etag", VersionID: "source-version"}, opts, 64<<20)
			if !handled || gets != 1 || puts != map[bool]int{true: 1, false: 0}[tc.write] || (err == nil) != tc.write {
				t.Fatalf("gets=%d puts=%d handled=%v err=%v", gets, puts, handled, err)
			}
		})
	}
}

package uploads

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/staging"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

// This is a protocol test of the actual official SDK and checked transport, not
// an assertion about any particular MinIO server accepting these requests.
func TestChecksumUploadOutboundSingleMultipartAndLegacy(t *testing.T) {
	for _, scenario := range []string{"single", "multipart", "native-rejection", "legacy-no-auto-crc", "response-checksum", "response-mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			payload := "body"
			if scenario == "multipart" {
				payload = strings.Repeat("x", 17*1024*1024)
			}
			spec := sha256Spec(payload)
			var mu sync.Mutex
			var puts, parts, completes, heads int
			var received int64
			committed := false
			fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				q := r.URL.Query()
				switch {
				case r.Method == http.MethodHead:
					heads++
					w.WriteHeader(404)
				case r.Method == http.MethodPost && q.Has("uploads"):
					fmt.Fprint(w, `<InitiateMultipartUploadResult><Bucket>bucket</Bucket><Key>file.bin</Key><UploadId>native-upload</UploadId></InitiateMultipartUploadResult>`)
				case r.Method == http.MethodPut:
					puts++
					h := md5.New()
					n, err := io.Copy(h, r.Body)
					if err != nil {
						t.Error("upstream body read", err)
						w.WriteHeader(500)
						return
					}
					received += n
					if scenario == "legacy-no-auto-crc" {
						for key := range r.Header {
							if strings.HasPrefix(strings.ToLower(key), "x-amz-checksum-") || strings.EqualFold(key, "x-amz-trailer") {
								t.Error("CRC/trailer unexpectedly enabled", r.Header)
							}
						}
						if len(r.Trailer) != 0 {
							t.Error("legacy upload unexpectedly added trailers", r.Trailer)
						}
					} else if got, want := r.Header.Get("Content-MD5"), base64.StdEncoding.EncodeToString(h.Sum(nil)); got != want {
						t.Errorf("native request checksum missing/wrong: %q != %q", got, want)
					}
					if q.Get("uploadId") != "" {
						parts++
					} else if r.Header.Get("If-None-Match") != "*" {
						t.Error("native single precondition lost", r.Header)
					}
					if scenario == "native-rejection" {
						w.WriteHeader(400)
						fmt.Fprint(w, `<Error><Code>BadDigest</Code><Message>checksum rejected</Message></Error>`)
						return
					}
					if scenario == "response-checksum" || scenario == "response-mismatch" {
						checksum := spec.Value
						if scenario == "response-mismatch" {
							checksum = sha256Spec("different").Value
						}
						w.Header().Set("x-amz-checksum-sha256", checksum)
						w.Header().Set("x-amz-checksum-type", "FULL_OBJECT")
					}
					w.Header().Set("ETag", `"`+hex.EncodeToString(h.Sum(nil))+`"`)
					if q.Get("uploadId") == "" {
						committed = true
					}
				case r.Method == http.MethodPost && q.Get("uploadId") != "":
					completes++
					if r.Header.Get("If-None-Match") != "*" {
						t.Error("native complete precondition lost", r.Header)
					}
					committed = true
					fmt.Fprint(w, `<CompleteMultipartUploadResult><Location>https://fixture/bucket/file.bin</Location><Bucket>bucket</Bucket><Key>file.bin</Key><ETag>"multipart-result"</ETag></CompleteMultipartUploadResult>`)
				default:
					t.Error("unexpected upstream request", r.Method, r.URL.Path, r.URL.RawQuery)
					w.WriteHeader(500)
				}
			}))
			defer fixture.Close()
			endpoint, err := url.Parse(fixture.URL)
			if err != nil {
				t.Fatal(err)
			}
			sdk, err := storage.NewS3(storage.Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4("test-key", "test-secret", ""), Region: "us-east-1"}, fixture.Client().Transport)
			if err != nil {
				t.Fatal(err)
			}
			client := storage.NewS3Client(sdk)
			budget := staging.New(int64(len(payload)))
			defer budget.Close()
			expected := &spec
			if scenario == "legacy-no-auto-crc" {
				expected = nil
			}
			value, err := WriteFileWithChecksum(context.Background(), budget, client, "bucket", "file.bin", "absent", "application/octet-stream", int64(len(payload)), strings.NewReader(payload), expected)
			mu.Lock()
			defer mu.Unlock()
			if scenario == "native-rejection" {
				if err == nil || apperror.Public(err).Code != "ChecksumMismatch" || committed || puts != 1 {
					t.Fatal("native failure retried/committed", value, err, committed, puts)
				}
				return
			}
			if err != nil || !committed || received != int64(len(payload)) || budget.Used() != 0 {
				t.Fatal(value, err, committed, received, budget.Used())
			}
			if scenario == "legacy-no-auto-crc" {
				if heads != 1 || value.Checksum != nil {
					t.Fatal("legacy behavior changed", heads, value)
				}
			} else {
				if heads != 2 || value.Checksum == nil {
					t.Fatal("destination/readback count changed", heads, value)
				}
				verification := "api_verified"
				if scenario == "response-checksum" {
					verification = "storage_confirmed"
				}
				if scenario == "response-mismatch" {
					verification = "storage_mismatch"
				}
				if value.Checksum.Verification != verification {
					t.Fatal(value.Checksum)
				}
				if scenario == "response-mismatch" && (value.OK || value.WriteState != "committed" || puts != 1) {
					t.Fatal("post-write mismatch became retryable", value, puts)
				}
			}
			if scenario == "multipart" && (parts != 2 || completes != 1 || puts != 2 || value.Checksum.RequestProtection != "PART_CONTENT_MD5") {
				t.Fatal("unexpected multipart protocol", parts, completes, puts, value)
			}
		})
	}
}

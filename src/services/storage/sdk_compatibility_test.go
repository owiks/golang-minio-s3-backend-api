package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"minio-web/api/src/services/objects/write"
	"minio-web/api/src/services/storage"
	"minio-web/api/src/services/uploads"
)

func compatibilityClient(t *testing.T, endpoint string, transport http.RoundTripper) *minio.Client {
	t.Helper()
	u, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	client, err := storage.NewS3(storage.Config{Endpoint: u, Credentials: credentials.NewStaticV4("fixture-key", "fixture-secret", ""), Region: "us-east-1"}, transport)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// Exercise PutObject's own multipart path, not a fake client that simply
// reflects PutObjectOptions. A condition on initiation alone does not protect
// the eventual object write.
func TestSDKConditionalUploadReachesFinalCommit(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		for _, multipart := range []bool{false, true} {
			for _, status := range []int{http.StatusOK, http.StatusPreconditionFailed, http.StatusConflict} {
				t.Run(fmt.Sprintf("overwrite=%t/multipart=%t/status=%d", overwrite, multipart, status), func(t *testing.T) {
					var starts, parts, commits, aborts atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path != "/fixture-bucket/file.txt" {
							t.Errorf("destination changed: %s", r.URL.Path)
						}
						_, _ = io.Copy(io.Discard, r.Body)
						switch {
						case r.Method == http.MethodPost && r.URL.Query().Has("uploads"):
							starts.Add(1)
							fmt.Fprint(w, `<InitiateMultipartUploadResult><Bucket>fixture-bucket</Bucket><Key>file.txt</Key><UploadId>fixture-upload</UploadId></InitiateMultipartUploadResult>`)
						case r.Method == http.MethodPut && r.URL.Query().Has("partNumber"):
							parts.Add(1)
							if r.URL.Query().Get("uploadId") != "fixture-upload" {
								t.Error("part upload ID changed")
							}
							w.Header().Set("ETag", `"fixture-part"`)
						case r.Method == http.MethodDelete:
							aborts.Add(1)
							w.WriteHeader(http.StatusNoContent)
						case r.Method == http.MethodPut || r.Method == http.MethodPost:
							commits.Add(1)
							if overwrite {
								if r.Header.Get("If-Match") != `"before"` || r.Header.Get("If-None-Match") != "" || !multipart && !strings.Contains(r.Header.Get("Authorization"), "if-match") {
									t.Errorf("signed overwrite precondition lost at final write: match=%q none=%q authorization=%q", r.Header.Get("If-Match"), r.Header.Get("If-None-Match"), r.Header.Get("Authorization"))
								}
							} else if r.Header.Get("If-None-Match") != "*" || r.Header.Get("If-Match") != "" || !multipart && !strings.Contains(r.Header.Get("Authorization"), "if-none-match") {
								t.Errorf("signed create precondition lost at final write: match=%q none=%q authorization=%q", r.Header.Get("If-Match"), r.Header.Get("If-None-Match"), r.Header.Get("Authorization"))
							}
							if status != http.StatusOK {
								w.WriteHeader(status)
								code := "PreconditionFailed"
								if status == http.StatusConflict {
									code = "ConditionalRequestConflict"
								}
								fmt.Fprintf(w, `<Error><Code>%s</Code></Error>`, code)
								return
							}
							w.Header().Set("ETag", `"saved"`)
							w.Header().Set("X-Amz-Version-Id", "fixture-version")
							if multipart {
								fmt.Fprint(w, `<CompleteMultipartUploadResult><Bucket>fixture-bucket</Bucket><Key>file.txt</Key><ETag>"saved"</ETag></CompleteMultipartUploadResult>`)
							}
						default:
							t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL)
							w.WriteHeader(http.StatusBadRequest)
						}
					}))
					defer server.Close()
					client := storage.NewS3Client(compatibilityClient(t, server.URL, nil))
					current := objectwrite.FileState{Revision: "checked", Exists: overwrite, ETag: "before"}
					opts, err := objectwrite.WriteOptions(current, "checked", "text/plain")
					if err != nil {
						t.Fatal(err)
					}
					size := int64(4)
					if multipart {
						size = int64(opts.PartSize) + 1
					}
					info, err := client.PutObject(context.Background(), "fixture-bucket", "file.txt", io.LimitReader(compatibilityZeroReader{}, size), size, opts)
					if status == http.StatusOK {
						if err != nil || info.ETag != "saved" || info.VersionID != "fixture-version" || info.Size != size {
							t.Fatal("upload result changed", info, err)
						}
					} else if err == nil {
						t.Fatal("native conflict became success")
					}
					wantStart, wantParts, wantAbort := int32(0), int32(0), int32(0)
					if multipart {
						wantStart, wantParts = 1, 2
						if status != http.StatusOK {
							wantAbort = 1
						}
					}
					if starts.Load() != wantStart || parts.Load() != wantParts || commits.Load() != 1 || aborts.Load() != wantAbort {
						t.Fatal("mutation repeated, downgraded or cleanup changed", starts.Load(), parts.Load(), commits.Load(), aborts.Load())
					}
				})
			}
		}
	}
}

type compatibilityZeroReader struct{}

func (compatibilityZeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

type compatibilityTransport func(*http.Request) (*http.Response, error)

func (f compatibilityTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSDKMutationsDoNotReplayFailures(t *testing.T) {
	for _, operation := range []string{"put", "initiate", "complete", "abort"} {
		for _, failure := range []string{"network", "redirect", "slow-down", "unavailable", "request-timeout", "access-denied"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				var calls atomic.Int32
				base := compatibilityTransport(func(r *http.Request) (*http.Response, error) {
					calls.Add(1)
					if r.Body != nil {
						_, _ = io.Copy(io.Discard, r.Body)
					}
					if failure == "network" {
						return nil, &net.OpError{Op: "read", Net: "tcp", Err: errors.New("fixture connection lost")}
					}
					status, code := http.StatusServiceUnavailable, "ServiceUnavailable"
					header := make(http.Header)
					switch failure {
					case "redirect":
						status, code = http.StatusTemporaryRedirect, "TemporaryRedirect"
						header.Set("Location", "http://other.invalid/fixture-bucket/file.txt")
					case "slow-down":
						status, code = http.StatusTooManyRequests, "SlowDown"
					case "request-timeout":
						status, code = http.StatusBadRequest, "RequestTimeout"
					case "access-denied":
						status, code = http.StatusForbidden, "AccessDenied"
					}
					return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(`<Error><Code>` + code + `</Code></Error>`))}, nil
				})
				client := compatibilityClient(t, "http://fixture.invalid", base)
				core := storage.NewS3Client(client)
				opts := minio.PutObjectOptions{DisableMultipart: true}
				opts.SetMatchETagExcept("*")
				ctx := context.Background()
				var err error
				switch operation {
				case "put":
					_, err = client.PutObject(ctx, "fixture-bucket", "file.txt", strings.NewReader("body"), 4, opts)
				case "initiate":
					_, err = core.NewMultipartUpload(ctx, "fixture-bucket", "file.txt", opts)
				case "complete":
					_, err = core.CompleteMultipartUpload(ctx, "fixture-bucket", "file.txt", "fixture-upload", []minio.CompletePart{{PartNumber: 1, ETag: "fixture-part"}}, opts)
				case "abort":
					err = core.AbortMultipartUpload(ctx, "fixture-bucket", "file.txt", "fixture-upload")
				}
				if err == nil || calls.Load() != 1 {
					t.Fatal("mutation failure lost or retried", calls.Load(), err)
				}
			})
		}
	}
}

func TestSDKCompleteRejectsEmbeddedErrorWithoutReplay(t *testing.T) {
	for _, code := range []string{"InternalError", "SlowDown", "PreconditionFailed", "ConditionalRequestConflict", "AccessDenied"} {
		t.Run(code, func(t *testing.T) {
			calls := 0
			client := compatibilityClient(t, "http://fixture.invalid", compatibilityTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodPost || r.URL.Query().Get("uploadId") != "fixture-upload" || r.Header.Get("If-None-Match") != "*" {
					t.Fatal("conditional completion changed")
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(" \n<Error><Code>" + code + "</Code></Error>"))}, nil
			}))
			opts := minio.PutObjectOptions{}
			opts.SetMatchETagExcept("*")
			info, err := storage.NewS3Client(client).CompleteMultipartUpload(context.Background(), "fixture-bucket", "file.txt", "fixture-upload", []minio.CompletePart{{PartNumber: 1, ETag: "fixture-part"}}, opts)
			if err == nil || minio.ToErrorResponse(err).Code != code || info.ETag != "" || calls != 1 {
				t.Fatal("HTTP 200 error accepted or replayed", info, calls, err)
			}
		})
	}
}

func TestSDKCompleteCancellationClosesResponse(t *testing.T) {
	started, closed := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		fmt.Fprint(w, `<CompleteMultipartUploadResult><Bucket>fixture-bucket</Bucket>`)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(closed)
	}))
	defer server.Close()
	client := compatibilityClient(t, server.URL, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := storage.NewS3Client(client).CompleteMultipartUpload(ctx, "fixture-bucket", "file.txt", "fixture-upload", []minio.CompletePart{{PartNumber: 1, ETag: "fixture-part"}}, minio.PutObjectOptions{})
		finished <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("completion did not reach response body")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation lost", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("completion did not stop")
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream response did not close")
	}
	if calls.Load() != 1 {
		t.Fatal("canceled mutation replayed", calls.Load())
	}
}

func TestSDKLegacyWriteFileAndReceiptRemainCompatible(t *testing.T) {
	var heads, puts atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			heads.Add(1)
			w.Header().Set("X-Minio-Error-Code", "NoSuchKey")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		puts.Add(1)
		data, err := io.ReadAll(r.Body)
		if err != nil || string(data) != "body" || r.Method != http.MethodPut || r.Header.Get("If-None-Match") != "*" || r.Header.Get("Content-Type") != "text/plain" {
			t.Error("legacy conditional upload changed", r.Method, string(data), r.Header.Get("If-None-Match"), r.Header.Get("Content-Type"), err)
		}
		w.Header().Set("ETag", `"saved"`)
		w.Header().Set("X-Amz-Version-Id", "fixture-version")
	}))
	defer server.Close()
	info, err := uploads.WriteFile(context.Background(), storage.NewS3Client(compatibilityClient(t, server.URL, server.Client().Transport)), "fixture-bucket", "file.txt", "absent", "text/plain", 4, strings.NewReader("body"))
	if err != nil || heads.Load() != 1 || puts.Load() != 1 {
		t.Fatal("legacy WriteFile changed", heads.Load(), puts.Load(), err)
	}
	raw, err := json.Marshal(uploads.UploadReceipt{OK: true, ETag: info.ETag, VersionID: info.VersionID, Size: info.Size})
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil || wire["ok"] != true || wire["etag"] != "saved" || wire["versionId"] != "fixture-version" || wire["size"] != float64(4) {
		t.Fatal("required receipt fields changed", string(raw), err)
	}
}

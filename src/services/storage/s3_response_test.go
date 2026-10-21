package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/upstream"
)

func s3Fixture(t *testing.T, tr http.RoundTripper) *minio.Client {
	t.Helper()
	endpoint, _ := url.Parse("http://fixture.invalid:9000")
	client, err := NewS3(Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4("fixture", "secret", ""), Region: "us-east-1"}, tr)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestS3ResponseClassification(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		status       int
		want         int64
	}{
		{"GET", "/", 200, maxS3MetadataResponse},
		{"GET", "/bucket", 200, maxS3MetadataResponse},
		{"GET", "/bucket/", 200, maxS3MetadataResponse},
		{"GET", "/bucket/?list-type=2&prefix=a%2F", 200, maxS3MetadataResponse},
		{"GET", "/bucket/key?uploadId=test", 200, maxS3MetadataResponse},
		{"GET", "/bucket/key?tagging", 200, maxS3MetadataResponse},
		{"GET", "/bucket/key?retention", 200, maxS3MetadataResponse},
		{"GET", "/bucket/key?legal-hold", 200, maxS3MetadataResponse},
		{"GET", "/bucket/key?attributes", 200, maxS3MetadataResponse},
		{"GET", "/bucket/key?future-subresource", 200, maxS3MetadataResponse},
		{"GET", "/bucket/key?bad=%zz", 200, maxS3MetadataResponse},
		{"GET", "/bucket/key", 200, 0},
		{"GET", "/bucket/key", 206, 0},
		{"GET", "/bucket/key?versionId=v1&partNumber=1&response-content-type=text%2Fplain", 200, 0},
		{"GET", "/bucket/key?X-Amz-Signature=s&X-Amz-Security-Token=t&X-Amz-Algorithm=a&X-Amz-Credential=c&X-Amz-Date=d&X-Amz-Expires=60&X-Amz-SignedHeaders=host", 200, 0},
		{"GET", "/bucket/key?AWSAccessKeyId=a&Signature=s&Expires=60&SecurityToken=t", 200, 0},
		{"GET", "/bucket/tagging%3Fpolicy%2Fpart", 200, 0},
		{"GET", "/bucket//", 200, 0},
		{"GET", "/bucket/key", 403, maxS3ErrorResponse},
		{"GET", "/bucket/key", 500, maxS3ErrorResponse},
		{"GET", "/bucket/key", 307, maxS3ErrorResponse},
		{"GET", "/bucket/key", 201, maxS3MetadataResponse},
		{"HEAD", "/bucket/key", 200, 0},
		{"HEAD", "/bucket/key", 403, 0},
		{"PUT", "/bucket/key", 200, maxS3MetadataResponse},
		{"POST", "/bucket/key?uploadId=test", 200, maxS3MetadataResponse},
		{"DELETE", "/bucket/key", 204, maxS3MetadataResponse},
	} {
		t.Run(fmt.Sprintf("%s %s %d", tc.method, tc.path, tc.status), func(t *testing.T) {
			u, err := url.Parse("http://fixture.invalid" + tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if got := s3ResponseLimit(tc.method, u, tc.status); got != tc.want {
				t.Fatal("wrong response classification", got, tc.want)
			}
		})
	}
}

func TestS3FiniteResponsesBoundAndCloseWithoutDraining(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		status       int
		limit        int64
	}{
		{"GET", "/bucket?policy", 200, maxS3MetadataResponse},
		{"GET", "/bucket?list-type=2", 200, maxS3MetadataResponse},
		{"GET", "/bucket/key?tagging", 200, maxS3MetadataResponse},
		{"POST", "/bucket/key?uploadId=test", 200, maxS3MetadataResponse},
		{"DELETE", "/bucket/key", 204, maxS3MetadataResponse},
		{"GET", "/bucket/key", 403, maxS3ErrorResponse},
	} {
		for _, length := range []string{"declared", "unknown", "understated"} {
			t.Run(tc.method+tc.path+"/"+length, func(t *testing.T) {
				body := &countedAdminBody{reader: io.LimitReader(zeroReader{}, tc.limit+4096)}
				declared := int64(-1)
				if length == "declared" {
					declared = tc.limit + 4096
				} else if length == "understated" {
					declared = 1
				}
				wire := s3ResponseTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: tc.status, Body: body, ContentLength: declared}, nil
				})}
				r, _ := http.NewRequest(tc.method, "http://fixture.invalid"+tc.path, nil)
				res, err := wire.RoundTrip(r)
				if res != nil {
					_ = res.Body.Close()
				}
				if err == nil || apperror.Public(err).Code != "S3ResponseTooLarge" || apperror.Public(err).Status != 502 || res != nil || body.closed != 1 {
					t.Fatal("response accepted or cleanup lost", err, body.closed)
				}
				if length == "declared" && body.read != 0 || int64(body.read) > tc.limit+1 {
					t.Fatal("read beyond limit", body.read, tc.limit)
				}
			})
		}
	}
}

func TestS3FiniteResponsesPreservePayloadAndHeaders(t *testing.T) {
	for _, status := range []int{200, 403} {
		limit := maxS3MetadataResponse
		if status == 403 {
			limit = maxS3ErrorResponse
		}
		for _, size := range []int{0, 27, limit} {
			t.Run(fmt.Sprintf("%d/%d", status, size), func(t *testing.T) {
				payload := bytes.Repeat([]byte("x"), size)
				body := &countedAdminBody{reader: bytes.NewReader(payload)}
				wire := s3ResponseTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: status, Header: http.Header{"X-Amz-Request-Id": {"native"}}, ContentLength: -1, Body: body}, nil
				})}
				r, _ := http.NewRequest("GET", "http://fixture.invalid/bucket?policy", nil)
				res, err := wire.RoundTrip(r)
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(res.Body)
				_ = res.Body.Close()
				if err != nil || !bytes.Equal(data, payload) || res.StatusCode != status || res.Header.Get("X-Amz-Request-Id") != "native" || body.closed != 1 {
					t.Fatal("payload or response metadata changed", err, body.closed)
				}
			})
		}
	}
}

type failedS3Read struct{ err error }

func (b failedS3Read) Read([]byte) (int, error) { return 0, b.err }

func TestS3SDKCannotDecodePrefixOrIgnoreCommandBodyFailure(t *testing.T) {
	for _, operation := range []string{"policy", "list", "tags", "set policy", "delete policy", "copy", "versions"} {
		for _, failure := range []error{io.ErrUnexpectedEOF, context.Canceled, context.DeadlineExceeded} {
			t.Run(operation+"/"+failure.Error(), func(t *testing.T) {
				ctx, trace := telemetry.Start(context.Background(), "S3 test")
				calls := 0
				body := &countedAdminBody{reader: io.MultiReader(strings.NewReader(`<ListBucketResult/>`), failedS3Read{err: failure})}
				base := roundTripFunc(func(*http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: 200, Header: http.Header{}, ContentLength: -1, Body: body}, nil
				})
				client := s3Fixture(t, WithContext(ctx, base))
				var err error
				switch operation {
				case "policy":
					_, err = client.GetBucketPolicy(ctx, "bucket")
				case "list":
					_, err = (minio.Core{Client: client}).ListObjectsV2("bucket", "", "", "", "/", 1000)
				case "tags":
					_, err = client.GetObjectTagging(ctx, "bucket", "key", minio.GetObjectTaggingOptions{})
				case "set policy":
					err = client.SetBucketPolicy(ctx, "bucket", `{}`)
				case "delete policy":
					err = client.SetBucketPolicy(ctx, "bucket", "")
				case "copy":
					_, err = (minio.Core{Client: client}).CopyObject(ctx, "bucket", "source", "bucket", "target", nil, minio.CopySrcOptions{}, minio.PutObjectOptions{})
				case "versions":
					_, err = ReadVersionPage(ctx, client, base, "bucket", "", "", "", 1000)
				}
				v := trace.UpstreamSnapshot()
				if !errors.Is(err, failure) || calls != 1 || body.closed != 1 || v.Attempts != 1 || v.Status != 200 || v.FailureStage != upstream.Body {
					t.Fatal("body failure masked, retried or counted twice", err, calls, body.closed, v)
				}
			})
		}
	}
}

func TestS3SDKCommandsAndObjectErrorsRejectOversizeWithoutRetry(t *testing.T) {
	for _, operation := range []string{"set policy", "delete policy", "copy", "get error", "versions"} {
		t.Run(operation, func(t *testing.T) {
			calls := 0
			status, limit := 200, maxS3MetadataResponse
			if operation == "get error" {
				status, limit = 503, maxS3ErrorResponse
			}
			body := &countedAdminBody{reader: io.LimitReader(zeroReader{}, int64(limit)+4096)}
			base := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, ContentLength: -1, Header: http.Header{}, Body: body}, nil
			})
			client := s3Fixture(t, base)
			ctx := context.Background()
			var err error
			switch operation {
			case "set policy":
				err = client.SetBucketPolicy(ctx, "bucket", `{}`)
			case "delete policy":
				err = client.SetBucketPolicy(ctx, "bucket", "")
			case "copy":
				_, err = (minio.Core{Client: client}).CopyObject(ctx, "bucket", "source", "bucket", "target", nil, minio.CopySrcOptions{}, minio.PutObjectOptions{})
			case "get error":
				_, _, _, err = (minio.Core{Client: client}).GetObject(ctx, "bucket", "key", minio.GetObjectOptions{})
			case "versions":
				_, err = ReadVersionPage(ctx, client, base, "bucket", "", "", "", 1000)
			}
			if err == nil || apperror.Public(err).Code != "S3ResponseTooLarge" || calls != 1 || body.closed != 1 || body.read > limit+1 {
				t.Fatal("size error ignored, retried or drained", err, calls, body.closed, body.read)
			}
		})
	}
}

func TestS3ObjectAndRangeBodiesStayLazy(t *testing.T) {
	for _, status := range []int{200, 206} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			body := &countedAdminBody{reader: io.LimitReader(zeroReader{}, maxS3MetadataResponse+1024)}
			client := s3Fixture(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/bucket/tagging?policy/part" || r.URL.Query().Get("versionId") != "v1" {
					t.Fatal("opaque object name or version changed", r.URL)
				}
				return &http.Response{StatusCode: status, ContentLength: 1 << 40, Header: http.Header{"Content-Type": {"application/xml"}, "Content-Length": {"1099511627776"}, "Last-Modified": {"Tue, 20 Oct 2026 10:00:00 GMT"}}, Body: body}, nil
			}))
			opts := minio.GetObjectOptions{VersionID: "v1"}
			if status == 206 {
				if err := opts.SetRange(0, 99); err != nil {
					t.Fatal(err)
				}
			}
			reader, _, _, err := (minio.Core{Client: client}).GetObject(context.Background(), "bucket", "tagging?policy/part", opts)
			if err != nil {
				t.Fatal(err)
			}
			if body.read != 0 || body.closed != 0 {
				t.Fatal("file buffered or closed before consumer read")
			}
			if n, err := reader.Read(make([]byte, 17)); err != nil || n != 17 || body.read != 17 {
				t.Fatal("stream read changed", n, err)
			}
			if n, err := io.CopyN(io.Discard, reader, maxS3MetadataResponse); err != nil || n != maxS3MetadataResponse {
				t.Fatal("object download capped by metadata limit", n, err)
			}
			_ = reader.Close()
			if body.closed != 1 || body.read != maxS3MetadataResponse+17 {
				t.Fatal("file drained or not closed")
			}
		})
	}
}

func TestS3UploadRequestStaysStreamingAndHeadAcceptsLargeObject(t *testing.T) {
	body := &countedAdminBody{reader: zeroReader{}}
	client := s3Fixture(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "HEAD" {
			return &http.Response{StatusCode: 200, ContentLength: 1 << 40, Header: http.Header{"Content-Length": {"1099511627776"}, "Last-Modified": {"Tue, 20 Oct 2026 10:00:00 GMT"}}, Body: http.NoBody}, nil
		}
		if r.Method != "PUT" || body.read != 0 || body.closed != 0 {
			t.Fatal("request body buffered before transport", r.Method, body.read, body.closed)
		}
		if n, err := io.CopyN(io.Discard, r.Body, maxS3MetadataResponse+1); err != nil || n != maxS3MetadataResponse+1 {
			t.Fatal("upload capped by response bound", n, err)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Etag": {`"native-etag"`}}, Body: http.NoBody}, nil
	}))
	ctx := context.Background()
	info, err := client.StatObject(ctx, "bucket", "key", minio.StatObjectOptions{})
	if err != nil || info.Size != 1<<40 {
		t.Fatal("HEAD length treated as metadata body", info.Size, err)
	}
	_, err = (minio.Core{Client: client}).PutObject(ctx, "bucket", "key", body, maxS3MetadataResponse+1, "", "", minio.PutObjectOptions{DisableContentSha256: true})
	if err != nil || body.read != maxS3MetadataResponse+1 {
		t.Fatal("upload changed", body.read, err)
	}
}

func TestS3SDKRejectsOversizedPolicyBeforeDecoding(t *testing.T) {
	endpoint, _ := url.Parse("http://fixture.invalid:9000")
	calls := 0
	client, err := NewS3(Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4("fixture", "secret", ""), Region: "us-east-1"}, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{}, ContentLength: -1, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", (4<<20)+1)))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	value, err := client.GetBucketPolicy(context.Background(), "bucket")
	if err == nil || value != "" || apperror.Public(err).Code != "S3ResponseTooLarge" || calls != 1 {
		t.Fatalf("policy accepted or retried: bytes=%d calls=%d err=%v", len(value), calls, err)
	}
}

func TestS3SDKRejectsCompleteXMLPrefixWithOversizedTail(t *testing.T) {
	for _, operation := range []string{"list", "versions", "tags"} {
		t.Run(operation, func(t *testing.T) {
			prefix := `<ListBucketResult><Name>bucket</Name></ListBucketResult>`
			if operation == "versions" {
				prefix = `<ListVersionsResult/>`
			} else if operation == "tags" {
				prefix = `<Tagging><TagSet/></Tagging>`
			}
			body := &countedAdminBody{reader: io.MultiReader(strings.NewReader(prefix), strings.NewReader(strings.Repeat(" ", maxS3MetadataResponse+1)))}
			base := roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, ContentLength: -1, Body: body}, nil
			})
			client := s3Fixture(t, base)
			var err error
			switch operation {
			case "list":
				_, err = (minio.Core{Client: client}).ListObjectsV2("bucket", "", "", "", "/", 1000)
			case "versions":
				_, err = ReadVersionPage(context.Background(), client, base, "bucket", "", "", "", 1000)
			case "tags":
				_, err = client.GetObjectTagging(context.Background(), "bucket", "key", minio.GetObjectTaggingOptions{})
			}
			if err == nil || apperror.Public(err).Code != "S3ResponseTooLarge" || body.closed != 1 || body.read != maxS3MetadataResponse+1 {
				t.Fatal("SDK accepted a prefix without checking the full bounded response", err, body.closed, body.read)
			}
		})
	}
}

func TestS3DefaultTransportAndCancellationDuringMetadataRead(t *testing.T) {
	started, released := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Version":"2012-10-17","Statement":[]}`))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(released)
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	client, err := NewS3(Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4("fixture", "secret", ""), Region: "us-east-1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := client.GetBucketPolicy(ctx, "bucket")
		done <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("SDK did not reach the response body")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("metadata buffering lost request cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("metadata body read did not cancel")
	}
	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled response body did not release the connection")
	}
}

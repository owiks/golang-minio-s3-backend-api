package files

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	objectssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

type previewTransport func(*http.Request) (*http.Response, error)

func (f previewTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestPreviewBoundsAndSourceConditions(t *testing.T) {
	for _, scenario := range []struct {
		name, body, code string
		size             int64
	}{
		{"normal", "text", "", 4}, {"empty", "", "", 0}, {"changed", "text", "ObjectChanged", 4},
		{"incomplete", "abc", "ObjectChanged", 4}, {"too-large", "", "PreviewTooLarge", objectssvc.MaxTextContent + 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			gets := 0
			transport := previewTransport(func(r *http.Request) (*http.Response, error) {
				h := http.Header{"Content-Length": {strconv.FormatInt(scenario.size, 10)}, "Content-Type": {"text/plain"}, "Etag": {`"original"`}, "Last-Modified": {time.Unix(1700000000, 0).UTC().Format(http.TimeFormat)}}
				status, body := 200, ""
				if r.Method == "GET" {
					gets++
					if r.Header.Get("If-Match") != `"original"` || r.Header.Get("Range") != "" {
						t.Error("preview must bind the current source without a zero-byte range")
					}
					body = scenario.body
					h.Set("Content-Length", strconv.Itoa(len(body)))
					if scenario.name == "changed" {
						status = 412
						body = "<Error><Code>PreconditionFailed</Code></Error>"
						h.Set("Content-Length", strconv.Itoa(len(body)))
						h.Set("Content-Type", "application/xml")
					}
				}
				length, _ := strconv.ParseInt(h.Get("Content-Length"), 10, 64)
				return &http.Response{StatusCode: status, Status: http.StatusText(status), Request: r, Header: h, ContentLength: length, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			client, err := minio.New("fixture.example", &minio.Options{Creds: credentials.NewStaticV4("key", "secret", ""), Region: "us-east-1", Transport: transport, MaxRetries: 1})
			if err != nil {
				t.Fatal(err)
			}
			result, err := GetPreview(context.Background(), storage.NewS3Client(client), "bucket-test", "file.txt")
			if scenario.code != "" {
				if err == nil || apperror.Public(err).Code != scenario.code {
					t.Fatal("wrong preview failure", result, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if string(result.Data) != scenario.body {
					t.Fatal(result)
				}
			}
			expectedGets := 1
			if scenario.name == "too-large" {
				expectedGets = 0
			}
			if gets != expectedGets {
				t.Fatal("unexpected body reads", gets)
			}
		})
	}
}

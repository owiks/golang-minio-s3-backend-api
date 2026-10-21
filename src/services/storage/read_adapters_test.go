package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
	"minio-web/api/src/services/apperror"
)

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

func TestReadAdaptersBoundBodiesAndRestorePayload(t *testing.T) {
	for _, kind := range []string{"configuration", "metrics"} {
		for _, oversized := range []bool{false, true} {
			t.Run(kind+"/"+map[bool]string{false: "normal", true: "oversized"}[oversized], func(t *testing.T) {
				limit := 2 << 20
				if kind == "configuration" {
					limit = 4 << 20
				}
				payload := "<root>original XML</root>"
				if oversized {
					payload = strings.Repeat("x", limit+1)
				}
				body := &trackedBody{Reader: strings.NewReader(payload)}
				base := roundTripFunc(func(*http.Request) (*http.Response, error) { return &http.Response{StatusCode: 200, Body: body}, nil })
				var captured []byte
				var tr http.RoundTripper = boundedMetricsTransport{Base: base, Data: &captured}
				var wire *configurationTransport
				if kind == "configuration" {
					wire = &configurationTransport{Base: base, Query: "cors"}
					tr = wire
				}
				req, _ := http.NewRequest("GET", "http://fixture.example/bucket?cors=", nil)
				res, err := tr.RoundTrip(req)
				if !body.closed {
					t.Fatal("upstream body not closed")
				}
				if oversized {
					if err == nil || apperror.Public(err).Status != 502 || res != nil {
						t.Fatal("oversized response accepted", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				restored, _ := io.ReadAll(res.Body)
				if wire != nil {
					captured = wire.Raw
					if wire.Status != 200 {
						t.Fatal("status lost")
					}
				}
				if string(restored) != payload || string(captured) != payload {
					t.Fatal("exact native payload lost")
				}
			})
		}
	}
}

func TestConfigurationAdapterRetainsSDKDeleteStatus(t *testing.T) {
	endpoint, _ := url.Parse("http://fixture.example:9000")
	cfg := Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4("test-access", "test-secret", ""), Region: "us-east-1"}
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "DELETE" || !r.URL.Query().Has("lifecycle") || r.Header.Get("Authorization") == "" {
			t.Fatal("wrong native delete")
		}
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`<Error><Code>AccessDenied</Code></Error>`)), Header: http.Header{}}, nil
	})
	status, _ := WriteLifecycleConfig(context.Background(), cfg, tr, "test-bucket", lifecycle.NewConfiguration())
	if status != 403 {
		t.Fatal("SDK DELETE rejection lost", status)
	}
}

func TestVersionAdapterSignsInternalEndpointAndPreservesQuery(t *testing.T) {
	endpoint, _ := url.Parse("http://internal.example:9000")
	client, err := NewS3(Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4("access", "secret", "session-token"), Region: "us-east-1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		q := r.URL.Query()
		if r.Context() != ctx || r.URL.Host != endpoint.Host || r.URL.Path != "/test-bucket/" || !q.Has("versions") || q.Get("prefix") != "a +%/" || q.Get("key-marker") != "a +%/key" || q.Get("version-id-marker") != "version+/=" || q.Get("max-keys") != "50" || q.Get("encoding-type") != "url" || q.Get("X-Amz-Security-Token") != "session-token" {
			t.Fatal("native signing/query changed")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`<ListVersionsResult><Version><Key>a%2B</Key><VersionId>v1</VersionId></Version><DeleteMarker><Key>a%2B</Key><VersionId>v2</VersionId></DeleteMarker><IsTruncated>true</IsTruncated><NextKeyMarker>a%2B</NextKeyMarker><NextVersionIdMarker>v2</NextVersionIdMarker></ListVersionsResult>`))}, nil
	})
	page, err := ReadVersionPage(ctx, client, tr, "test-bucket", "a +%/", "a +%/key", "version+/=", 50)
	if err != nil || len(page.Rows) != 2 || page.Rows[0].Deleted || !page.Rows[1].Deleted || !page.Truncated || page.VersionMarker != "v2" {
		t.Fatal("mixed version order/markers changed", page, err)
	}
	for _, body := range []string{`<Error><Code>AccessDenied</Code></Error>`, `not XML`} {
		tr = func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(body))}, nil
		}
		_, err = ReadVersionPage(ctx, client, tr, "test-bucket", "", "", "", 50)
		if body == "not XML" {
			if !errors.Is(err, ErrInvalidVersionResponse) {
				t.Fatal(err)
			}
		} else if native := minio.ToErrorResponse(err); native.Code != "AccessDenied" || native.StatusCode != 403 {
			t.Fatal("native error lost", err)
		}
	}
}

package storage

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
)

type deleteTransport func(*http.Request) (*http.Response, error)

func (f deleteTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestConditionalDeleteDoesNotDowngrade(t *testing.T) {
	endpoint, _ := url.Parse("http://127.0.0.1:19000")
	cfg := Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4("test-key", "test-secret", ""), Region: "us-east-1"}
	calls := 0
	current := "new"
	transport := deleteTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "DELETE" || r.Header.Get("If-Match") != `"old"` || !strings.Contains(r.URL.Query().Get("X-Amz-SignedHeaders"), "if-match") || r.URL.Query().Has("versionId") {
			t.Fatal("deletion condition was not signed")
		}
		status := 204
		if r.Header.Get("If-Match") != `"`+current+`"` {
			status = 412
		} else {
			current = ""
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})
	err := ConditionalDelete(context.Background(), cfg, transport, "bucket-test", "file", "old", false)
	if apperror.Public(err).Code != "ConditionalDeleteUnavailable" || calls != 0 {
		t.Fatal("unconfirmed server contacted")
	}
	err = ConditionalDelete(context.Background(), cfg, transport, "bucket-test", "file", "old", true)
	if apperror.Public(err).Code != "SourceChanged" || current != "new" || calls != 1 {
		t.Fatal("racing source was deleted", err)
	}
	current = "old"
	if err = ConditionalDelete(context.Background(), cfg, transport, "bucket-test", "file", "old", true); err != nil || current != "" || calls != 2 {
		t.Fatal(err)
	}
}
func TestNativeConditionalDeleteOrigins(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{{"https://s3.amazonaws.com", true}, {"https://s3.eu-west-1.amazonaws.com", true}, {"https://s3.cn-north-1.amazonaws.com.cn", true}, {"http://s3.amazonaws.com", false}, {"https://s3-website.eu-west-1.amazonaws.com", false}, {"https://s3.amazonaws.com.evil.test", false}, {"https://minio.example", false}} {
		endpoint, _ := url.Parse(tc.raw)
		if NativeConditionalDelete(endpoint) != tc.want {
			t.Fatal(tc.raw)
		}
	}
}

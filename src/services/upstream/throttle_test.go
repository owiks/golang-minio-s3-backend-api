package upstream_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/upstream"
)

func TestThrottleClassificationOnlyExplicitLoadLimits(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"slowdown", minio.ErrorResponse{Code: "SlowDown", StatusCode: 503}, true},
		{"slowdown-read", minio.ErrorResponse{Code: "SlowDownRead", StatusCode: 503}, true},
		{"code-without-status", minio.ErrorResponse{Code: "SlowDown"}, true},
		{"wrapped-pointer", fmt.Errorf("wrapped: %w", &minio.ErrorResponse{Code: "SlowDown", StatusCode: 503}), true},
		{"wrapped-value", fmt.Errorf("wrapped: %w", minio.ErrorResponse{StatusCode: 429}), true},
		{"generic-service-unavailable", minio.ErrorResponse{Code: "ServiceUnavailable", StatusCode: 503}, false},
		{"write-limit", minio.ErrorResponse{Code: "SlowDownWrite", StatusCode: 503}, false},
		{"authorization", minio.ErrorResponse{Code: "AccessDenied", StatusCode: 403}, false},
		{"conflicting-auth-status", minio.ErrorResponse{Code: "AccessDenied", StatusCode: 429}, false},
		{"conflicting-slowdown-status", minio.ErrorResponse{Code: "SlowDown", StatusCode: 403}, false},
		{"cancelled", errors.Join(minio.ErrorResponse{Code: "SlowDown", StatusCode: 503}, context.Canceled), false},
		{"deadline", errors.Join(minio.ErrorResponse{StatusCode: 429}, context.DeadlineExceeded), false},
		{"network-error", errors.New("429 SlowDown private upstream text"), false},
		{"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := upstream.IsThrottled(tc.err); got != tc.want {
				t.Fatalf("classification = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestS3LoadLimitsSurviveSDKWithoutHiddenRetries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"slowdown", 503, `<Error><Code>SlowDown</Code><Message>private resource</Message></Error>`, true},
		{"plain-429", 429, `private rate limit response`, true},
		{"generic-503", 503, `<Error><Code>ServiceUnavailable</Code></Error>`, false},
		{"forbidden", 403, `<Error><Code>AccessDenied</Code></Error>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			endpoint, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			client, err := storage.NewS3(storage.Config{Endpoint: endpoint, Region: "us-east-1", Credentials: credentials.NewStaticV4("fixture-key", "fixture-secret", "")}, server.Client().Transport)
			if err != nil {
				t.Fatal(err)
			}
			_, err = (minio.Core{Client: client}).ListObjectsV2("bucket", "", "", "", "", 500)
			var response minio.ErrorResponse
			if err == nil || !errors.As(err, &response) || response.StatusCode != tc.status {
				t.Fatalf("SDK did not preserve status %d", tc.status)
			}
			if got := upstream.IsThrottled(err); got != tc.want || calls.Load() != 1 {
				t.Fatalf("classification=%v want=%v calls=%d", got, tc.want, calls.Load())
			}
		})
	}
}

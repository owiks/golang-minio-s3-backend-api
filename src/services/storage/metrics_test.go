package storage

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
)

func TestMetricsCapturePreservesSDKAuthenticationAndNativeErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"metrics", "minio_s3_requests_total{api=\"getobject\"} 42\n", http.StatusOK},
		{"empty", "", http.StatusOK},
		{"malformed", "invalid metric text", http.StatusOK},
		{"denied", `{"Code":"AccessDenied","Message":"fixture refusal"}`, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, trace := telemetry.Start(context.Background(), "GET metrics")
			endpoint, _ := url.Parse("http://fixture.invalid:9000")
			cfg := Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4("fixture-access", "fixture-secret", "fixture-session")}
			body := &trackedBody{Reader: strings.NewReader(tc.body)}
			calls := 0
			base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Context() != ctx || r.Method != http.MethodGet || r.URL.Host != endpoint.Host || r.URL.Path != "/minio/v2/metrics/cluster" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || r.Header.Get("X-Amz-Security-Token") != "fixture-session" {
					t.Fatal("SDK metrics authentication, context or destination changed")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: body}, nil
			})
			data, err := ReadS3Metrics(ctx, cfg, base)
			if calls != 1 || !body.closed {
				t.Fatal("metrics request repeated or original body not closed", calls, body.closed)
			}
			observed := trace.UpstreamSnapshot()
			if observed.Attempts != 1 || observed.Status != tc.status {
				t.Fatal("original upstream observation lost", observed)
			}
			if tc.status == http.StatusOK {
				if err != nil || string(data) != tc.body || observed.Failures != 0 {
					t.Fatal("raw metrics were parsed by the SDK or capture became an upstream failure", err, observed)
				}
				return
			}
			if err == nil || madmin.ToErrorResponse(err).Code != "AccessDenied" || data != nil || observed.Failures == 0 {
				t.Fatal("native authorization failure lost", err, observed)
			}
		})
	}
}

func TestMetricsCaptureDoesNotTurnBodyFailureIntoSuccess(t *testing.T) {
	for _, tc := range []struct {
		name string
		body io.ReadCloser
	}{
		{"oversized", &trackedBody{Reader: strings.NewReader(strings.Repeat("x", (2<<20)+1))}},
		{"read failure", brokenResponseBody{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint, _ := url.Parse("http://fixture.invalid:9000")
			cfg := Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4("fixture-access", "fixture-secret", "")}
			calls := 0
			base := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: tc.body}, nil
			})
			data, err := ReadS3Metrics(context.Background(), cfg, base)
			if err == nil || data != nil || calls != 1 {
				t.Fatal("failed body capture was accepted or retried", err, calls)
			}
		})
	}
}

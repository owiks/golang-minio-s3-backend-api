package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"minio-web/api/src/services/telemetry"
	"minio-web/api/src/services/upstream"
)

type brokenResponseBody struct{}

func (brokenResponseBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (brokenResponseBody) Close() error             { return nil }

func TestConsumingAdaptersObserveOriginalBodyBeforeBuffering(t *testing.T) {
	for _, kind := range []string{"configuration", "metrics"} {
		t.Run(kind, func(t *testing.T) {
			ctx, trace := telemetry.Start(context.Background(), "GET test")
			calls := 0
			base := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Body: brokenResponseBody{}, ContentLength: 4}, nil
			})
			var wire http.RoundTripper = &configurationTransport{Base: base, Query: "cors"}
			if kind == "metrics" {
				wire = boundedMetricsTransport{Base: base}
			}
			r, _ := http.NewRequestWithContext(ctx, "GET", "http://fixture.invalid/bucket?cors", nil)
			response, err := observeTransport(wire).RoundTrip(r)
			if response != nil {
				_ = response.Body.Close()
			}
			v := trace.UpstreamSnapshot()
			if !errors.Is(err, io.ErrUnexpectedEOF) || calls != 1 || v.Attempts != 1 || v.Status != 200 || v.FailureClass != "read" || v.FailureStage != upstream.Body {
				t.Fatalf("buffered adapter lost original response/body cause: %+v err=%v", v, err)
			}
		})
	}
}

func TestCoreContextBindingObservesOneAttempt(t *testing.T) {
	ctx, trace := telemetry.Start(context.Background(), "GET objects")
	endpoint, _ := url.Parse("http://fixture.invalid:9000")
	calls := 0
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if telemetry.From(r.Context()) != trace {
			t.Fatal("request-scoped context lost")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`<ListBucketResult><Name>bucket</Name><IsTruncated>false</IsTruncated></ListBucketResult>`)), Header: http.Header{}}, nil
	})
	// Mirrors runtime.requestS3, including its existing inner observation wrapper.
	client, err := NewS3(Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4("fixture", "secret", ""), Region: "us-east-1"}, WithContext(ctx, telemetry.Transport{Base: base}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (minio.Core{Client: client}).ListObjectsV2("bucket", "", "", "", "/", 1); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || trace.UpstreamSnapshot().Attempts != 1 || trace.UpstreamSnapshot().Status != 200 {
		t.Fatal("Core network call missing or double counted", calls, trace.UpstreamSnapshot())
	}
}

func TestAdminObservationKeepsOriginalStatusNotSyntheticStop(t *testing.T) {
	for _, kind := range []string{"HTTP", "body", "SDK", "before network"} {
		t.Run(kind, func(t *testing.T) {
			ctx, trace := telemetry.Start(context.Background(), "POST admin")
			calls := 0
			invalidJSON, err := madmin.EncryptData("fixture-secret", []byte("invalid JSON payload"))
			if err != nil {
				t.Fatal(err)
			}
			client := adminFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if kind == "body" {
					return &http.Response{StatusCode: 502, Body: brokenResponseBody{}}, nil
				}
				if kind == "SDK" {
					return adminReply(200, string(invalidJSON)), nil
				}
				return adminReply(502, `{"Code":"InternalError","Message":"PRIVATE-ADMIN-SECRET"}`), nil
			}))
			switch kind {
			case "SDK":
				_, err = client.AddServiceAccount(ctx, madmin.AddServiceAccountReq{AccessKey: "child-fixture", SecretKey: "fixture-secret"})
			case "before network":
				_, err = client.AddServiceAccount(ctx, madmin.AddServiceAccountReq{Name: strings.Repeat("x", 1025)})
			default:
				err = client.AddUser(ctx, "fixture-user", "fixture-secret")
			}
			if err == nil {
				t.Fatal("expected SDK/native failure")
			}
			v := trace.UpstreamSnapshot()
			if kind == "before network" {
				if calls != 0 || v.Attempts != 0 || v.FailureClass != "sdk" || v.FailureAttempt != 0 || v.Status != 0 {
					t.Fatal("local SDK failure invented an attempt", v)
				}
				return
			}
			if calls != 1 || v.Attempts != 1 || v.Status == 400 {
				t.Fatal("retried or synthetic status logged", calls, v)
			}
			if kind == "HTTP" && (v.Status != 502 || v.FailureClass != "http" || v.FailureStatus != 502 || v.FailureAttempt != 1) {
				t.Fatal("original HTTP evidence lost", v)
			}
			if kind == "body" && (v.Status != 502 || v.FailureClass != "read" || v.FailureStage != upstream.Body) {
				t.Fatal("admin body error masked", v)
			}
			if kind == "SDK" && (v.Status != 200 || v.FailureClass != "sdk" || v.FailureStage != upstream.SDK || v.FailureAttempt != 0 || v.FailureStatus != 0) {
				t.Fatal("SDK decode error misattributed", v)
			}
		})
	}
}

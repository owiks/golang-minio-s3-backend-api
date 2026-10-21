package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/capabilities"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func capabilityConnection(t *testing.T, handler http.HandlerFunc) (*runtime.State, *runtime.Session, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet {
			t.Errorf("capability check mutated storage: %s", r.Method)
			http.Error(w, "unexpected write", 500)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	app := runtime.New(config.Config{})
	t.Cleanup(app.Close)
	session, err := runtime.NewConnectedSession(runtime.Connection{AccessKey: "fixture-access", SecretKey: "fixture-secret", Region: "us-east-1"}, app.Config(), u, u, &http.Transport{})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.StoreSession(session); err != nil {
		t.Fatal(err)
	}
	return app, session, calls
}

func TestCapabilitiesHTTPUsesValidatedSDKReadsAndSafeReasons(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   capabilities.Status
		reason string
	}{
		{"supported", 200, `<GetObjectAttributesOutput><ETag>opaque</ETag><ObjectSize>3</ObjectSize><StorageClass>STANDARD</StorageClass></GetObjectAttributesOutput>`, capabilities.Supported, "OperationSucceeded"},
		{"denied", 403, `<Error><Code>AccessDenied</Code><Message>SECRET details</Message></Error>`, capabilities.AccessDenied, "AccessDenied"},
		{"unsupported", 501, `<Error><Code>NotImplemented</Code><Message>SECRET details</Message></Error>`, capabilities.Unsupported, "OperationNotImplemented"},
		{"missing", 404, `<Error><Code>NoSuchKey</Code><Message>SECRET details</Message></Error>`, capabilities.Unknown, "ObjectNotFound"},
		{"invalid200", 200, `<Error><Code>InternalError</Code></Error>`, capabilities.Unknown, "InvalidResponse"},
		{"brokenXML", 200, `<GetObjectAttributesOutput><ETag>`, capabilities.Unknown, "InvalidResponse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, session, calls := capabilityConnection(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/bucket/file.csv" || !r.URL.Query().Has("attributes") {
					t.Errorf("unexpected probe %s", r.URL)
				}
				w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			})
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/capabilities?feature=object.attributes&bucket=bucket&key=file.csv", nil)
			if err := HandleCapabilities(app, w, r, session); err != nil {
				t.Fatal(err)
			}
			var result capabilities.Snapshot
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err, w.Body.String())
			}
			if len(result.Capabilities) != 1 || result.Capabilities[0].Status != tc.want || result.Capabilities[0].Reason != tc.reason || calls.Load() != 1 {
				t.Fatal(result, calls.Load())
			}
			if w.Code != 200 || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
				t.Fatal(w.Code, w.Header())
			}
			// Identical requests reuse only this authenticated session's evidence.
			if err := HandleCapabilities(app, httptest.NewRecorder(), r, session); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 {
				t.Fatal("cache did not bound network calls", calls.Load())
			}
		})
	}
}

func TestCapabilitiesHTTPUnknownWritesAndTimeoutNeverBecomeSupport(t *testing.T) {
	app, session, calls := capabilityConnection(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	w := httptest.NewRecorder()
	if err := HandleCapabilities(app, w, httptest.NewRequest("GET", "/capabilities?feature=object.put.conditional&bucket=bucket&key=file", nil), session); err != nil {
		t.Fatal(err)
	}
	var result capabilities.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Capabilities[0].Status != capabilities.Unknown || calls.Load() != 0 {
		t.Fatal("write probe sent", result, calls.Load())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	r := httptest.NewRequest("GET", "/capabilities?feature=object.attributes&bucket=bucket&key=file", nil).WithContext(ctx)
	if err := HandleCapabilities(app, httptest.NewRecorder(), r, session); err == nil {
		t.Fatal("canceled read became success")
	}
}

func TestCapabilitiesHTTPWholeCatalogOnlySendsTwoBoundedGETs(t *testing.T) {
	app, session, calls := capabilityConnection(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Has("attributes"):
			if r.Header.Get("X-Amz-Max-Parts") != "1" {
				t.Errorf("unbounded attributes page: %v", r.Header)
			}
			w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
			_, _ = fmt.Fprint(w, `<GetObjectAttributesOutput><ETag>opaque</ETag><ObjectSize>3</ObjectSize></GetObjectAttributesOutput>`)
		case q.Has("uploads"):
			if q.Get("max-uploads") != "1" {
				t.Errorf("unbounded multipart list: %v", q)
			}
			_, _ = fmt.Fprint(w, `<ListMultipartUploadsResult><Bucket>bucket</Bucket><IsTruncated>false</IsTruncated></ListMultipartUploadsResult>`)
		default:
			t.Errorf("unexpected probe %s", r.URL)
		}
	})
	w := httptest.NewRecorder()
	if err := HandleCapabilities(app, w, httptest.NewRequest("GET", "/capabilities?bucket=bucket&key=file.csv", nil), session); err != nil {
		t.Fatal(err)
	}
	var result capabilities.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || len(result.Capabilities) != len(capabilities.Identifiers()) {
		t.Fatal(calls.Load(), result)
	}
	for _, value := range result.Capabilities {
		if value.ID != capabilities.ObjectAttributes && value.ID != capabilities.MultipartList && value.Status != capabilities.Unknown {
			t.Fatal("unobserved mutation reported support", value)
		}
	}
}

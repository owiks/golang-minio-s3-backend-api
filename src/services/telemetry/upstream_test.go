package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/upstream"
)

type observedBody struct {
	reads, closes int
	err, closeErr error
}

func (b *observedBody) Read(p []byte) (int, error) { b.reads++; return copy(p, "x"), b.err }
func (b *observedBody) Close() error               { b.closes++; return b.closeErr }

func tracedRequest(t *testing.T) (*http.Request, *Trace) {
	t.Helper()
	ctx, trace := Start(context.Background(), "GET static-route")
	r, err := http.NewRequestWithContext(ctx, "GET", "https://PRIVATE-ENDPOINT/bucket/PRIVATE-KEY?X-Amz-Signature=PRIVATE-SIGNATURE", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "PRIVATE-CREDENTIAL")
	return r, trace
}

func TestTransportBodyFailuresPreserveReadCloseAndEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, class string
		err         error
	}{
		{"truncated", "read", io.ErrUnexpectedEOF},
		{"timeout", "timeout", context.DeadlineExceeded},
		{"cancelled", "cancelled", context.Canceled},
		{"unknown read", "read", errors.New("PRIVATE-RAW-BODY-ERROR")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, trace := tracedRequest(t)
			closeErr := errors.New("PRIVATE-CLOSE-ERROR")
			body := &observedBody{err: tc.err, closeErr: closeErr}
			response := &http.Response{StatusCode: 200, Body: body, ContentLength: -1}
			got, err := (Transport{Base: transportFunc(func(*http.Request) (*http.Response, error) { return response, nil })}).RoundTrip(r)
			if err != nil || got != response || body.reads != 0 {
				t.Fatal("response eagerly consumed or changed")
			}
			buf := make([]byte, 1)
			for range 2 {
				if n, err := got.Body.Read(buf); n != 1 || err != tc.err || string(buf) != "x" {
					t.Fatal("read contract changed")
				}
			}
			if got.Body.Close() != closeErr || body.closes != 1 {
				t.Fatal("close changed or not forwarded")
			}
			SDKError(r.Context(), apperror.Public(tc.err))
			Error(r.Context(), apperror.Public(tc.err))
			v := trace.UpstreamSnapshot()
			if v.Attempts != 1 || v.Status != 200 || v.Failures != 1 || v.FailureClass != tc.class || v.FailureStage != upstream.Body || v.FailureStatus != 200 || v.FailureAttempt != 1 {
				t.Fatalf("wire/body evidence lost or counted twice: %+v", v)
			}
		})
	}
}

func TestEOFAndIntentionalClose(t *testing.T) {
	for _, scenario := range []string{"complete", "short EOF", "HEAD", "204", "304", "early close"} {
		t.Run(scenario, func(t *testing.T) {
			r, trace := tracedRequest(t)
			body := io.NopCloser(strings.NewReader("body"))
			resp := &http.Response{StatusCode: 200, ContentLength: 4, Body: body}
			if scenario != "complete" {
				resp.ContentLength = 8
			}
			switch scenario {
			case "HEAD":
				r.Method = "HEAD"
			case "204":
				resp.StatusCode = 204
			case "304":
				resp.StatusCode = 304
			}
			got, err := (Transport{Base: transportFunc(func(*http.Request) (*http.Response, error) { return resp, nil })}).RoundTrip(r)
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "early close" {
				data, err := io.ReadAll(got.Body)
				if err != nil || string(data) != "body" {
					t.Fatal("EOF semantics changed", err)
				}
			}
			_ = got.Body.Close()
			want := 0
			if scenario == "short EOF" {
				want = 1
			}
			if v := trace.UpstreamSnapshot(); v.Failures != want {
				t.Fatal("false or missing truncation", scenario, v)
			}
		})
	}
}

func TestParallelUpstreamStatusAndLateReadFailure(t *testing.T) {
	r, trace := tracedRequest(t)
	started, proceed := make(chan struct{}), make(chan struct{})
	first := Transport{Base: transportFunc(func(*http.Request) (*http.Response, error) {
		close(started)
		<-proceed
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("error"))}, nil
	})}
	var wg sync.WaitGroup
	wg.Go(func() {
		response, err := first.RoundTrip(r)
		if err != nil {
			t.Error(err)
			return
		}
		_ = response.Body.Close()
	})
	<-started
	response, err := (Transport{Base: transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})}).RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	close(proceed)
	wg.Wait()
	v := trace.UpstreamSnapshot()
	if v.Attempts != 2 || v.Status != 200 || v.FailureStatus != 503 || v.FailureAttempt != 1 || v.FailureClass != "http" {
		t.Fatalf("parallel response attributed to wrong attempt: %+v", v)
	}
}

func TestDiagnosticsAreBoundedSanitizedAndSeparateFromPublicErrors(t *testing.T) {
	r, trace := tracedRequest(t)
	failure := &url.Error{Op: "GET", URL: r.URL.String(), Err: &net.DNSError{Name: "PRIVATE-HOST", Err: "PRIVATE-DNS-ERROR"}}
	response, err := (Transport{Base: transportFunc(func(*http.Request) (*http.Response, error) { return nil, failure })}).RoundTrip(r)
	if response != nil {
		_ = response.Body.Close()
	}
	if err != failure {
		t.Fatal("transport error identity changed")
	}
	problem := apperror.Public(err)
	Error(r.Context(), problem)
	Failure(r.Context(), problem.Code)
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)
	trace.Log(r.Context(), problem.Status, 1, time.Second)
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["upstream_failure_class"] != "dns" || record["upstream_failure_stage"] != "request" || record["upstream_failure_attempt"] != float64(1) || record["upstream_status"] != float64(0) {
		t.Fatal("diagnostic log missing evidence", output.String())
	}
	public, _ := json.Marshal(problem)
	if strings.Contains(output.String(), "PRIVATE-") || strings.Contains(string(public), "PRIVATE-") || strings.Contains(string(public), "cause") || strings.Contains(string(public), "dns") || problem.Code != "UpstreamError" || problem.Status != 502 {
		t.Fatal("secret or diagnostics leaked into public envelope")
	}
	// A successful request may have encountered a transient failure or a
	// permission probe: diagnostics must not turn it into an API failure.
	if trace.ResultName(r.Context(), 200) != "error" {
		t.Fatal("explicit Failure lost")
	}
}

func TestMappedSDKCausesAndLocalValidation(t *testing.T) {
	ctx, trace := Start(context.Background(), "PUT static-route")
	Error(ctx, apperror.New(400, "InvalidName", messages.Literal("safe")))
	if trace.UpstreamSnapshot().Failures != 0 {
		t.Fatal("local validation attributed to upstream")
	}
	SDKError(ctx, errors.New("PRIVATE-SDK-DECODE"))
	Error(ctx, apperror.Public(errors.New("PRIVATE-SDK-DECODE")))
	v := trace.UpstreamSnapshot()
	if v.FailureClass != "sdk" || v.FailureStage != upstream.SDK || v.Attempts != 0 || v.FailureAttempt != 0 || v.FailureStatus != 0 || v.Failures != 1 {
		t.Fatal("SDK error duplicated or invented network attempt", v)
	}
	if trace.ResultName(ctx, 200) != "success" {
		t.Fatal("diagnostic observation changed domain outcome")
	}
}

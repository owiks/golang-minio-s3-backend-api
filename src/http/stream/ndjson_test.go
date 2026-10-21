package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/result"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/upstream"
)

type completion struct {
	OK bool `json:"ok"`
}

func TestNDJSONRetainsMappedUpstreamCauseOnlyInTelemetry(t *testing.T) {
	for _, producerEvent := range []bool{false, true} {
		t.Run(fmt.Sprint(producerEvent), func(t *testing.T) {
			ctx, trace := telemetry.Start(context.Background(), "POST stream")
			w := httptest.NewRecorder()
			WriteNDJSON(ctx, w, func(_ context.Context, send func(result.StreamEvent[completion]) bool) error {
				problem := apperror.Public(&net.DNSError{Name: "PRIVATE-ENDPOINT", Err: "PRIVATE-DNS-CAUSE"})
				if producerEvent {
					send(result.StreamEvent[completion]{Error: problem})
					return nil
				}
				return problem
			})
			v := trace.UpstreamSnapshot()
			if v.FailureClass != "dns" || v.FailureStage != upstream.Operation || v.FailureAttempt != 0 || v.Failures != 1 || v.Attempts != 0 {
				t.Fatal("stream cause lost or duplicated", v)
			}
			if w.Code != 200 || strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(w.Body.String(), "cause") || !strings.Contains(w.Body.String(), `"code":"UpstreamError"`) || trace.ResultName(ctx, 200) != "error" {
				t.Fatal("NDJSON contract or sanitization changed", w.Body.String())
			}
		})
	}
}

type eventWriter struct {
	*httptest.ResponseRecorder
	writes, flushes    int
	writeErr, flushErr error
}

func (w *eventWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return w.ResponseRecorder.Write(p)
}
func (w *eventWriter) FlushError() error {
	w.flushes++
	if w.flushErr != nil {
		return w.flushErr
	}
	w.ResponseRecorder.Flush()
	return nil
}

func TestNDJSONEnvelopeHeadersAndSingleTerminalEvent(t *testing.T) {
	w := &eventWriter{ResponseRecorder: httptest.NewRecorder()}
	parent, trace := telemetry.Start(context.Background(), "test")
	var operation context.Context
	var lateSend func(result.StreamEvent[completion]) bool
	WriteNDJSON(parent, w, func(ctx context.Context, send func(result.StreamEvent[completion]) bool) error {
		operation, lateSend = ctx, send
		if !send(result.StreamEvent[completion]{Progress: &result.Progress{Bytes: 0, Total: 4}}) || !send(result.StreamEvent[completion]{Result: &completion{OK: true}}) {
			t.Fatal("valid events rejected")
		}
		if send(result.StreamEvent[completion]{Progress: &result.Progress{Bytes: 4, Total: 4}}) {
			t.Fatal("progress sent after completion")
		}
		return apperror.New(409, "LateFailure", messages.Literal("must not append a second terminal event"))
	})
	want := "{\"progress\":{\"bytes\":0,\"total\":4}}\n{\"result\":{\"ok\":true}}\n"
	if w.Code != 200 || w.Body.String() != want || w.writes != 2 || w.flushes != 2 || w.Header().Get("Content-Type") != "application/x-ndjson; charset=utf-8" || w.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatal("wire format or flush behavior changed", w.Body.String(), w.Header())
	}
	if operation.Err() != context.Canceled || parent.Err() != nil || lateSend(result.StreamEvent[completion]{Result: &completion{}}) || trace.ResultName(parent, 200) != "success" {
		t.Fatal("stream lifetime or completed outcome changed")
	}
}

func TestNDJSONSafeTerminalErrorAndDomainOutcome(t *testing.T) {
	for _, cause := range []error{apperror.New(409, "DestinationChanged", messages.Literal("Файл изменился.")), errors.New("PRIVATE-UPSTREAM-CREDENTIALS")} {
		w := &eventWriter{ResponseRecorder: httptest.NewRecorder()}
		ctx, trace := telemetry.Start(context.Background(), "test")
		WriteNDJSON(ctx, w, func(ctx context.Context, send func(result.StreamEvent[completion]) bool) error {
			send(result.StreamEvent[completion]{Progress: &result.Progress{Bytes: 0, Total: 4}})
			return cause
		})
		if w.Code != 200 || strings.Contains(w.Body.String(), "PRIVATE-") || strings.Count(w.Body.String(), `"error"`) != 1 || !strings.Contains(w.Body.String(), `"code":"`+apperror.Public(cause).Code+`"`) || trace.ResultName(ctx, 200) != "error" {
			t.Fatal("unsafe or missing terminal error", w.Body.String())
		}
	}
	ctx, trace := telemetry.Start(context.Background(), "test")
	WriteNDJSON(ctx, httptest.NewRecorder(), func(ctx context.Context, send func(result.StreamEvent[completion]) bool) error {
		telemetry.Outcome(ctx, "error", "ChecksumMismatch")
		send(result.StreamEvent[completion]{Result: &completion{OK: false}})
		return nil
	})
	if trace.ResultName(ctx, 200) != "error" || trace.Code != "ChecksumMismatch" {
		t.Fatal("domain mismatch replaced with delivery success")
	}
}

func TestNDJSONWriteAndFlushFailuresCancelWithoutRetry(t *testing.T) {
	for _, where := range []string{"write", "flush"} {
		t.Run(where, func(t *testing.T) {
			w := &eventWriter{ResponseRecorder: httptest.NewRecorder()}
			if where == "write" {
				w.writeErr = io.ErrClosedPipe
			} else {
				w.flushErr = io.ErrClosedPipe
			}
			ctx, trace := telemetry.Start(context.Background(), "test")
			WriteNDJSON(ctx, w, func(ctx context.Context, send func(result.StreamEvent[completion]) bool) error {
				if send(result.StreamEvent[completion]{Progress: &result.Progress{}}) || ctx.Err() != context.Canceled {
					t.Fatal("delivery failure did not cancel producer")
				}
				// Even a producer ignoring the callback cannot append or erase failure.
				telemetry.Outcome(ctx, "success", "")
				if send(result.StreamEvent[completion]{Result: &completion{OK: true}}) {
					t.Fatal("write retried")
				}
				return apperror.New(502, "AnotherFailure", messages.Literal("failed"))
			})
			wantFlushes := 0
			if where == "flush" {
				wantFlushes = 1
			}
			if w.writes != 1 || w.flushes != wantFlushes || trace.ResultName(ctx, 200) != "cancelled" || trace.Code != "StreamWriteFailed" {
				t.Fatal("failure retried or hidden", w.writes, w.flushes, trace.Code)
			}
		})
	}
}

func TestNDJSONDoesNotWriteToCancelledRequest(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	ctx, trace := telemetry.Start(parent, "test")
	w := &eventWriter{ResponseRecorder: httptest.NewRecorder()}
	WriteNDJSON(ctx, w, func(ctx context.Context, send func(result.StreamEvent[completion]) bool) error {
		if send(result.StreamEvent[completion]{Progress: &result.Progress{}}) {
			t.Fatal("event sent after request cancellation")
		}
		return ctx.Err()
	})
	if w.writes != 0 || w.flushes != 0 || w.Body.Len() != 0 || trace.ResultName(ctx, 200) != "cancelled" {
		t.Fatal("cancelled request generated response bytes")
	}
}

func TestNDJSONConcurrentEventsRemainWholeLines(t *testing.T) {
	w := &eventWriter{ResponseRecorder: httptest.NewRecorder()}
	WriteNDJSON(context.Background(), w, func(ctx context.Context, send func(result.StreamEvent[completion]) bool) error {
		var group sync.WaitGroup
		for i := range 16 {
			group.Go(func() {
				if !send(result.StreamEvent[completion]{Progress: &result.Progress{Bytes: int64(i), Total: 16}}) {
					t.Error("concurrent progress rejected")
				}
			})
		}
		group.Wait()
		send(result.StreamEvent[completion]{Result: &completion{OK: true}})
		return nil
	})
	if lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n"); len(lines) != 17 {
		t.Fatal("events interleaved or lost", fmt.Sprint(lines))
	} else {
		for _, line := range lines {
			if !strings.HasPrefix(line, `{"progress":{`) && line != `{"result":{"ok":true}}` {
				t.Fatal("invalid event line", line)
			}
		}
	}
}

// ResponseController must work through middleware wrappers, not type assertions
// against the immediate writer's optional Flusher/deadline interfaces.
type unwrappedWriter struct{ target http.ResponseWriter }

func (w unwrappedWriter) Header() http.Header         { return w.target.Header() }
func (w unwrappedWriter) WriteHeader(status int)      { w.target.WriteHeader(status) }
func (w unwrappedWriter) Write(p []byte) (int, error) { return w.target.Write(p) }
func (w unwrappedWriter) Unwrap() http.ResponseWriter { return w.target }

func TestNDJSONFlushUnwrapsResponseWriter(t *testing.T) {
	w := &eventWriter{ResponseRecorder: httptest.NewRecorder()}
	WriteNDJSON(context.Background(), unwrappedWriter{target: w}, func(ctx context.Context, send func(result.StreamEvent[completion]) bool) error {
		if !send(result.StreamEvent[completion]{Result: &completion{OK: true}}) {
			t.Fatal("wrapper prevented flush")
		}
		return nil
	})
	if w.flushes != 1 {
		t.Fatal("underlying writer not flushed")
	}
}

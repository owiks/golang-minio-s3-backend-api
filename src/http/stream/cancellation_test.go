package stream

import (
	"context"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/result"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
)

type deadlineWriter struct {
	header                           http.Header
	phase                            string
	entered, unblock                 chan struct{}
	enter, release                   sync.Once
	mu                               sync.Mutex
	deadlines                        int
	deadlineEntered, deadlineRelease chan struct{}
}

func newDeadlineWriter(phase string) *deadlineWriter {
	return &deadlineWriter{header: http.Header{}, phase: phase, entered: make(chan struct{}), unblock: make(chan struct{})}
}
func (w *deadlineWriter) Header() http.Header { return w.header }
func (w *deadlineWriter) WriteHeader(int)     {}
func (w *deadlineWriter) block() error {
	w.enter.Do(func() { close(w.entered) })
	<-w.unblock
	return os.ErrDeadlineExceeded
}
func (w *deadlineWriter) Write(p []byte) (int, error) {
	if w.phase == "write" {
		return 0, w.block()
	}
	return len(p), nil
}
func (w *deadlineWriter) FlushError() error {
	if w.phase == "flush" {
		return w.block()
	}
	return nil
}
func (w *deadlineWriter) SetWriteDeadline(deadline time.Time) error {
	if deadline.After(time.Now()) {
		return nil
	}
	w.mu.Lock()
	w.deadlines++
	w.mu.Unlock()
	if w.deadlineEntered != nil {
		close(w.deadlineEntered)
		<-w.deadlineRelease
	}
	w.release.Do(func() { close(w.unblock) })
	return nil
}
func (w *deadlineWriter) deadlineCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.deadlines
}

func TestCancellationInterruptsBlockedNDJSONWriteAndFlush(t *testing.T) {
	for _, phase := range []string{"write", "flush"} {
		t.Run(phase, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx, trace := telemetry.Start(parent, "test")
			w := newDeadlineWriter(phase)
			defer w.release.Do(func() { close(w.unblock) })
			done := make(chan struct{})
			go func() {
				defer close(done)
				WriteNDJSON(ctx, unwrappedWriter{target: w}, func(ctx context.Context, send func(result.StreamEvent[completion]) bool) error {
					if send(result.StreamEvent[completion]{Progress: &result.Progress{}}) || ctx.Err() == nil {
						t.Error("blocked delivery continued after cancellation")
					}
					return ctx.Err()
				})
			}()
			select {
			case <-w.entered:
			case <-time.After(time.Second):
				t.Fatal("response did not block")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("cancelled stream still blocked downstream")
			}
			if w.deadlineCount() != 1 || trace.ResultName(ctx, 200) != "cancelled" {
				t.Fatal("cancellation deadline or outcome lost")
			}
		})
	}
}

func TestInterruptCleanupWaitsForRunningDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newDeadlineWriter("")
	w.deadlineEntered, w.deadlineRelease = make(chan struct{}), make(chan struct{})
	cleanup := InterruptOnCancel(ctx, unwrappedWriter{target: w})
	cancel()
	select {
	case <-w.deadlineEntered:
	case <-time.After(time.Second):
		t.Fatal("deadline callback not started")
	}
	done := make(chan struct{})
	go func() { cleanup(); close(done) }()
	select {
	case <-done:
		t.Error("cleanup returned with a pending deadline")
	case <-time.After(20 * time.Millisecond):
	}
	close(w.deadlineRelease)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not finish")
	}
	cleanup()
	if w.deadlineCount() != 1 {
		t.Fatal("cleanup repeated deadline callback")
	}
}

func TestNormalCompletionAndPanicStopCancellationHook(t *testing.T) {
	for _, crash := range []bool{false, true} {
		w := newDeadlineWriter("")
		var operation context.Context
		func() {
			defer func() {
				if p := recover(); (p != nil) != crash {
					t.Error("producer panic not preserved", p)
				}
			}()
			WriteNDJSON(context.Background(), w, func(ctx context.Context, send func(result.StreamEvent[completion]) bool) error {
				operation = ctx
				if crash {
					panic("test panic")
				}
				send(result.StreamEvent[completion]{Result: &completion{OK: true}})
				return nil
			})
		}()
		if operation.Err() != context.Canceled || w.deadlineCount() != 0 {
			t.Fatal("cleanup leaked context or changed a later response deadline")
		}
	}
}

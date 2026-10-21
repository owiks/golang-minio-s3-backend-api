package storage

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiagnosticCloseDrainsSDKTerminalSendsOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan error)
	producerDone := make(chan struct{})
	var cancels atomic.Int32
	stream := newDiagnosticStream(events, func() { cancels.Add(1); cancel() }, func() int { return 403 }, nil)
	if stream.Events() != events || stream.Status() != 403 {
		t.Fatal("stream metadata lost")
	}
	go func() {
		defer close(producerDone)
		defer close(events)
		<-ctx.Done()
		// Matches SDK trace decoding error followed by a failed reconnect: these
		// sends do not consult ctx.Done and must have a receiver after HTTP returns.
		events <- context.Canceled
		events <- context.Canceled
	}()
	var callers sync.WaitGroup
	for range 8 {
		callers.Go(stream.Close)
	}
	callers.Wait()
	select {
	case <-producerDone:
	case <-time.After(time.Second):
		t.Fatal("SDK producer remains blocked after Close")
	}
	if cancels.Load() != 1 {
		t.Fatal("Close is not idempotent", cancels.Load())
	}
}

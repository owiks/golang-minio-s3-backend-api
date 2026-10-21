package identities

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestSummaryQueueStopsPendingReadsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	entered := make(chan struct{}, 4)
	done := make(chan error, 1)
	go func() {
		done <- ReadSummaryRows(ctx, 10000, func(int) { calls.Add(1); entered <- struct{}{}; <-ctx.Done() })
	}()
	for range 4 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("workers not started")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("queue did not stop")
	}
	if calls.Load() != 4 {
		t.Fatal("cancelled queue started pending reads", calls.Load())
	}
	if err := ReadSummaryRows(ctx, 10, func(int) { t.Error("read after cancellation") }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

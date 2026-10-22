package operations

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestScanThrottleDelayHasFiniteIncreasingBudget(t *testing.T) {
	expected := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, time.Minute, time.Minute, time.Minute}
	for i, want := range expected {
		if got := ScanThrottleDelay(i + 1); got != want {
			t.Fatalf("retry %d: got %v, want %v", i+1, got, want)
		}
	}
	for _, invalid := range []int{-1, 0, MaxScanThrottleRetries + 1, int(^uint(0) >> 1)} {
		if got := ScanThrottleDelay(invalid); got != 0 {
			t.Fatalf("invalid retry %d produced delay %v", invalid, got)
		}
	}
	if MaxScanThrottleDeferrals < MaxScanThrottleRetries || !errors.Is(fmt.Errorf("step: %w", ErrThrottled), ErrThrottled) {
		t.Fatal("invalid throttling contract")
	}
}

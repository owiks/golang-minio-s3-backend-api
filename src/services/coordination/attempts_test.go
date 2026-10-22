package coordination

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This is the previous implementation's contract oracle. It exercises clock
// changes and retry rounding independently of rate.Limiter's internal state.
type attemptBudget struct {
	tokens  float64
	updated time.Time
}

func (b *attemptBudget) allow(now time.Time, burst int, interval time.Duration) (bool, time.Duration) {
	elapsed := max(0, now.Sub(b.updated).Seconds())
	b.tokens = min(float64(burst), b.tokens+elapsed/interval.Seconds())
	b.updated = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) * float64(interval))
}

func TestAttemptsPreserveBudgetAndRetryContract(t *testing.T) {
	for _, interval := range []time.Duration{time.Second, 3 * time.Second, time.Minute, 1500 * time.Millisecond, 7 * time.Nanosecond, 100000002 * time.Nanosecond} {
		t.Run(interval.String(), func(t *testing.T) {
			const burst = 3
			now := time.Unix(100, 0)
			a := NewAttempts(burst, 1, interval)
			previous := attemptBudget{tokens: burst, updated: now}
			// Include repeated rejection, partial refill, backward time, and a
			// refill boundary after the rejected clock has moved backward.
			offsets := []time.Duration{0, 0, 0, 0, interval / 3, interval / 3, interval / 2, interval / 4, interval / 2, interval, interval, interval + interval/3, 2 * interval}
			for _, offset := range offsets {
				at := now.Add(offset)
				wantAllowed, wantWait := previous.allow(at, burst, interval)
				allowed, wait := a.Allow("client", at)
				// Division and the limiter's multiplication by the reciprocal
				// may differ by one nanosecond at fractional boundaries. The
				// HTTP Retry-After header must remain identical.
				if allowed != wantAllowed || absDuration(wait-wantWait) > time.Nanosecond || retrySeconds(wait) != retrySeconds(wantWait) {
					t.Fatalf("at %v: got (%v, %v), want (%v, %v)", offset, allowed, wait, wantAllowed, wantWait)
				}
			}
		})
	}
}

func TestAttemptsDifferentialClockSequences(t *testing.T) {
	random := rand.New(rand.NewPCG(1, 2))
	for range 100 {
		interval := 100*time.Millisecond + time.Duration(random.Int64N(int64(time.Minute-100*time.Millisecond)))
		burst := 2 + random.IntN(10)
		now := time.Unix(100, 0)
		a := NewAttempts(burst, 1, interval)
		previous := attemptBudget{tokens: float64(burst), updated: now}
		for i := range 1000 {
			// Keep each idle gap below expiration, and periodically move the
			// supplied clock back to exercise the legacy checkpoint contract.
			step := time.Duration(random.Int64N(int64(interval / 2)))
			if i%17 == 0 {
				step = -step
			}
			now = now.Add(step)
			wantAllowed, wantWait := previous.allow(now, burst, interval)
			allowed, wait := a.Allow("client", now)
			if allowed != wantAllowed || absDuration(wait-wantWait) > time.Nanosecond || retrySeconds(wait) != retrySeconds(wantWait) {
				t.Fatalf("interval %v, attempt %d: got (%v, %v), want (%v, %v)", interval, i, allowed, wait, wantAllowed, wantWait)
			}
		}
	}
}

func TestAttemptsRejectedRequestsDoNotReserveFutureTokens(t *testing.T) {
	a := NewAttempts(1, 1, time.Second)
	now := time.Unix(100, 0)
	if allowed, _ := a.Allow("client", now); !allowed {
		t.Fatal("initial token rejected")
	}
	for i := range 1000 {
		at := now.Add(time.Duration(i) * time.Millisecond)
		if allowed, _ := a.Allow("client", at); allowed {
			t.Fatalf("attempt %d borrowed a future token", i)
		}
	}
	if allowed, _ := a.Allow("client", now.Add(time.Second-time.Nanosecond)); allowed {
		t.Fatal("sub-nanosecond rounding borrowed a future token")
	}
	if allowed, _ := a.Allow("client", now.Add(time.Second)); !allowed {
		t.Fatal("rejected requests consumed the next refill")
	}
}

func TestAttemptsExactRefillBoundary(t *testing.T) {
	interval := 100000002 * time.Nanosecond
	a := NewAttempts(2, 1, interval)
	now := time.Unix(100, 0)
	a.Allow("client", now)
	a.Allow("client", now)
	if allowed, _ := a.Allow("client", now.Add(interval)); !allowed {
		t.Fatal("reciprocal rounding rejected the exact refill boundary")
	}
}

func TestAttemptsIdleExpirationUsesLastAttempt(t *testing.T) {
	a := NewAttempts(2, 2, time.Second)
	now := time.Unix(100, 0)
	a.Allow("active", now)
	a.Allow("active", now)
	a.Allow("idle", now)
	if allowed, _ := a.Allow("active", now.Add(time.Second/2)); allowed {
		t.Fatal("rejected request unexpectedly accepted")
	}
	if allowed, _ := a.Allow("new", now.Add(2*time.Second)); !allowed {
		t.Fatal("idle principal did not expire at burst*interval")
	}
	if len(a.entries) != 2 || a.entries["active"].limiter == nil || a.entries["idle"].limiter != nil {
		t.Fatal("rejected activity was not retained or idle principal survived")
	}
	if allowed, wait := a.Allow("overflow", now.Add(2*time.Second)); allowed || wait != time.Second {
		t.Fatalf("table did not fail closed: %v %v", allowed, wait)
	}
}

func TestAttemptsPrincipalTableLimit(t *testing.T) {
	const capacity = 4096
	a := NewAttempts(1, capacity, time.Second)
	now := time.Unix(100, 0)
	for i := range capacity {
		if allowed, _ := a.Allow(fmt.Sprint(i), now); !allowed {
			t.Fatalf("principal %d rejected before table capacity", i)
		}
	}
	for range 2 {
		if allowed, wait := a.Allow("overflow", now); allowed || wait != time.Second || len(a.entries) != capacity {
			t.Fatalf("full table changed: %v %v entries=%d", allowed, wait, len(a.entries))
		}
	}
	if allowed, _ := a.Allow("overflow", now.Add(time.Second)); !allowed || len(a.entries) != 1 {
		t.Fatal("expired table was not reclaimed")
	}
}

func TestAttemptsConcurrentBurstIsAtomic(t *testing.T) {
	const burst = 20
	a := NewAttempts(burst, 1, time.Second)
	now := time.Unix(100, 0)
	var allowed atomic.Int32
	var jobs sync.WaitGroup
	for range 100 {
		jobs.Go(func() {
			if ok, _ := a.Allow("client", now); ok {
				allowed.Add(1)
			}
		})
	}
	jobs.Wait()
	if got := allowed.Load(); got != burst {
		t.Fatalf("concurrent calls consumed %d tokens, want %d", got, burst)
	}
}

func retrySeconds(wait time.Duration) int {
	return max(1, int(math.Ceil(wait.Seconds())))
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

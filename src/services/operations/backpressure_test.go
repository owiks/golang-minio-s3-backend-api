package operations

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func backpressureInsert(t *testing.T, r *Memory, scope Scope, spec Spec, deadline, now time.Time) Snapshot {
	t.Helper()
	s, err := r.Insert(context.Background(), scope, spec, deadline, now)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func backpressureClaim(t *testing.T, r *Memory, now time.Time) Snapshot {
	t.Helper()
	s, err := r.Claim(context.Background(), "worker", now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func backpressureSave(t *testing.T, r *Memory, s Snapshot, c Checkpoint, now time.Time) Snapshot {
	t.Helper()
	s, err := r.Save(context.Background(), s.ID, s.LeaseID, s.Revision, c, now)
	if err != nil || !Valid(s) {
		t.Fatal("invalid backpressure transition", s, err)
	}
	return s
}

func TestBackpressurePreservesCheckpointAndConnectionFenceAcrossBackup(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := NewMemory()
	scope := Scope{"first", "shared"}
	backpressureInsert(t, r, scope, Spec{Scan: &ScanSpec{Bucket: "data"}}, now.Add(time.Hour), now)
	s := backpressureClaim(t, r, now)
	backpressureSave(t, r, s, Checkpoint{Page: &ScanPage{Objects: 3, Bytes: 99, NextToken: "checked-cursor"}}, now)
	s = backpressureClaim(t, r, now)
	s = backpressureSave(t, r, s, Checkpoint{Throttle: true}, now)
	if s.State != Queued || s.ThrottleConsecutive != 1 || s.ThrottleTotal != 1 || !s.NotBefore.Equal(now.Add(2*time.Second)) || s.Scan.Pages != 1 || s.Scan.Objects != 3 || s.Scan.Bytes != 99 || s.Scan.Token != "checked-cursor" || len(s.Scan.Seen) != 1 {
		t.Fatal("throttle changed checkpoint or budget", s)
	}
	backup, err := r.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r = NewMemory()
	if err = r.Import(ctx, backup); err != nil {
		t.Fatal("paused backup rejected", err)
	}
	second := backpressureInsert(t, r, Scope{"new-actor", "shared"}, transferSpec(1), now.Add(time.Hour), now.Add(time.Microsecond))
	other := backpressureInsert(t, r, Scope{"other", "independent"}, Spec{Scan: &ScanSpec{Bucket: "data"}}, now.Add(time.Hour), now.Add(2*time.Microsecond))
	claimed := backpressureClaim(t, r, now.Add(3*time.Microsecond))
	if claimed.ID != other.ID {
		t.Fatal("connection pause blocked unrelated connection or allowed bypass", claimed.ID)
	}
	_ = backpressureSave(t, r, claimed, Checkpoint{Page: &ScanPage{}}, now.Add(3*time.Microsecond))
	s, err = r.Cancel(ctx, scope, s.ID, s.Revision, now.Add(4*time.Microsecond))
	if err != nil || s.State != Cancelled || s.NotBefore.IsZero() {
		t.Fatal("cancellation removed connection fence", s, err)
	}
	for i := range MaxPerActor + 2 {
		stamp := now.Add(time.Duration(i+5) * time.Microsecond)
		v := backpressureInsert(t, r, scope, transferSpec(1), now.Add(time.Hour), stamp)
		if _, err = r.Cancel(ctx, scope, v.ID, v.Revision, stamp); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = r.Get(ctx, scope, s.ID); err != nil {
		t.Fatal("capacity eviction removed active connection fence", err)
	}
	if until, err := r.Cooldown(ctx, "shared", now.Add(time.Second)); err != nil || !until.Equal(s.NotBefore) {
		t.Fatal("restored/cancelled fence lost", until, err)
	}
	if _, err = r.Claim(ctx, "worker", now.Add(time.Second), time.Second); !errors.Is(err, ErrNotFound) {
		t.Fatal("new actor bypassed shared connection pause", err)
	}
	claimed = backpressureClaim(t, r, s.NotBefore)
	if claimed.ID != second.ID {
		t.Fatal("transfer did not resume after pause", claimed.ID)
	}
	if until, err := r.Cooldown(ctx, "shared", s.NotBefore); err != nil || !until.IsZero() {
		t.Fatal("elapsed pause still active", until, err)
	}
}

func TestBackpressureRecoveryDoesNotHideExpiredMutationOrDuplicateScan(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := NewMemory()
	readScope := Scope{"reader", "shared"}
	backpressureInsert(t, r, readScope, Spec{Scan: &ScanSpec{Bucket: "data"}}, now.Add(time.Hour), now)
	read := backpressureClaim(t, r, now)
	read = backpressureSave(t, r, read, Checkpoint{Execute: true}, now)
	mutationScope := Scope{"writer", "shared"}
	backpressureInsert(t, r, mutationScope, transferSpec(1), now.Add(time.Hour), now.Add(time.Microsecond))
	mutation := backpressureClaim(t, r, now.Add(time.Microsecond))
	mutation = backpressureSave(t, r, mutation, Checkpoint{Begin: true}, now.Add(time.Microsecond))
	backpressureInsert(t, r, Scope{"limiter", "shared"}, Spec{Scan: &ScanSpec{Bucket: "data"}}, now.Add(time.Hour), now.Add(2*time.Microsecond))
	paused := backpressureClaim(t, r, now.Add(2*time.Microsecond))
	paused = backpressureSave(t, r, paused, Checkpoint{Throttle: true}, now.Add(2*time.Microsecond))
	if _, err := r.Claim(ctx, "recovery", now.Add(time.Second+time.Microsecond), time.Second); !errors.Is(err, ErrNotFound) {
		t.Fatal("paused execution reclaimed", err)
	}
	mutation, err := r.Get(ctx, mutationScope, mutation.ID)
	if err != nil || mutation.State != Unknown || len(mutation.Items) != 1 || mutation.Items[0].State != Unknown {
		t.Fatal("pause hid uncertain mutation", mutation, err)
	}
	if _, err = r.Retry(ctx, mutationScope, mutation.ID, mutation.Revision, now.Add(2*time.Second)); !errors.Is(err, ErrConflict) {
		t.Fatal("uncertain mutation allowed retry", err)
	}
	read, err = r.Get(ctx, readScope, read.ID)
	if err != nil || read.State != Queued || read.LeaseID != "" || read.InFlight || !read.NotBefore.Equal(paused.NotBefore) || !Valid(read) {
		t.Fatal("expired scan was not safely requeued behind pause", read, err)
	}
	waiting, execution, known := read.Durations(now.Add(2 * time.Second))
	if !known || waiting != time.Second || execution != time.Second {
		t.Fatal("crash recovery inflated active execution", waiting, execution)
	}
}

func TestBackpressureFiniteConsecutiveBudgetAndExplicitRetry(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := NewMemory()
	scope := Scope{"reader", "connection"}
	s := backpressureInsert(t, r, scope, Spec{Scan: &ScanSpec{Bucket: "data"}}, now.Add(time.Hour), now)
	for attempt := 1; attempt <= MaxScanThrottleRetries; attempt++ {
		s = backpressureClaim(t, r, now)
		s = backpressureSave(t, r, s, Checkpoint{Throttle: true}, now)
		if s.State != Queued || s.ThrottleConsecutive != attempt || s.ThrottleTotal != attempt || s.NotBefore.Sub(now) != ScanThrottleDelay(attempt) || s.Scan.Pages != 0 {
			t.Fatal("incorrect automatic retry budget", attempt, s)
		}
		now = s.NotBefore
	}
	s = backpressureClaim(t, r, now)
	s = backpressureSave(t, r, s, Checkpoint{Throttle: true}, now)
	if s.State != Failed || s.Code != "ScanThrottled" || s.ThrottleTotal != MaxScanThrottleRetries || s.ThrottleConsecutive != MaxScanThrottleRetries {
		t.Fatal("exhausted scan retry budget was not terminal", s)
	}
	fence := s.NotBefore
	s, err := r.Retry(ctx, scope, s.ID, s.Revision, now)
	if err != nil || s.ThrottleConsecutive != 0 || s.ThrottleTotal != MaxScanThrottleRetries || !s.NotBefore.Equal(fence) {
		t.Fatal("explicit retry must restart consecutive allowance without erasing total", s, err)
	}
	if _, err = r.Claim(ctx, "worker", now, time.Second); !errors.Is(err, ErrNotFound) {
		t.Fatal("explicit retry bypassed shared upstream pause", err)
	}
	now = fence
	s = backpressureClaim(t, r, now)
	s = backpressureSave(t, r, s, Checkpoint{Throttle: true}, now)
	if s.State != Queued || s.ThrottleConsecutive != 1 || s.ThrottleTotal != MaxScanThrottleRetries+1 || s.NotBefore.Sub(now) != 2*time.Second {
		t.Fatal("explicit safe retry cannot make progress", s)
	}
}

func TestBackpressureConcurrentCancellationKeepsObservedConnectionPause(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := NewMemory()
	scope := Scope{"reader", "shared"}
	backpressureInsert(t, r, scope, Spec{Scan: &ScanSpec{Bucket: "data"}}, now.Add(time.Hour), now)
	s := backpressureClaim(t, r, now)
	s = backpressureSave(t, r, s, Checkpoint{Execute: true}, now)
	s, err := r.Cancel(ctx, scope, s.ID, s.Revision, now.Add(time.Microsecond))
	if err != nil || !s.CancelRequested || s.State != Running {
		t.Fatal("running scan cancellation not recorded", s, err)
	}
	stamp := now.Add(2 * time.Microsecond)
	s = backpressureSave(t, r, s, Checkpoint{Throttle: true}, stamp)
	if s.State != Cancelled || !s.NotBefore.Equal(stamp.Add(2*time.Second)) || s.Scan.Pages != 0 || s.ThrottleTotal != 0 {
		t.Fatal("concurrent cancellation lost observed load limit", s)
	}
	transfer := backpressureInsert(t, r, Scope{"new-writer", "shared"}, transferSpec(1), now.Add(time.Hour), now.Add(3*time.Microsecond))
	if _, err = r.Claim(ctx, "worker", now.Add(time.Second), time.Second); !errors.Is(err, ErrNotFound) {
		t.Fatal("new actor bypassed cancelled scan's load limit", err)
	}
	if until, cooldownErr := r.Cooldown(ctx, "shared", now.Add(time.Second)); cooldownErr != nil || !until.Equal(s.NotBefore) {
		t.Fatal("cancelled scan fence absent from scheduler", until, cooldownErr)
	}
	if claimed := backpressureClaim(t, r, s.NotBefore); claimed.ID != transfer.ID {
		t.Fatal("transfer stayed blocked after observed load pause", claimed.ID)
	}
}

func TestBackpressureSuccessfulPagesResetConsecutiveButNotTotalBudget(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := NewMemory()
	s := backpressureInsert(t, r, Scope{"reader", "connection"}, Spec{Scan: &ScanSpec{Bucket: "data"}}, now.Add(time.Hour), now)
	for count := 1; count <= MaxScanThrottleDeferrals; count++ {
		s = backpressureClaim(t, r, now)
		s = backpressureSave(t, r, s, Checkpoint{Throttle: true}, now)
		now = s.NotBefore
		s = backpressureClaim(t, r, now)
		s = backpressureSave(t, r, s, Checkpoint{Page: &ScanPage{Objects: 1, Bytes: 2, NextToken: fmt.Sprintf("cursor-%d", count)}}, now)
		if s.ThrottleConsecutive != 0 || !s.NotBefore.IsZero() || s.ThrottleTotal != count || s.Scan.Pages != count {
			t.Fatal("successful page erased total or kept consecutive pause", count, s)
		}
	}
	s = backpressureClaim(t, r, now)
	s = backpressureSave(t, r, s, Checkpoint{Throttle: true}, now)
	if s.State != Failed || s.Code != "ScanThrottled" || s.ThrottleTotal != MaxScanThrottleDeferrals || s.Scan.Pages != MaxScanThrottleDeferrals || s.Scan.Objects != MaxScanThrottleDeferrals {
		t.Fatal("intermittent success bypassed lifetime retry budget", s)
	}
	ctx := context.Background()
	_ = backpressureInsert(t, r, Scope{"new-writer", "connection"}, transferSpec(1), now.Add(time.Hour), now)
	if _, err := r.Claim(ctx, "worker", now, time.Second); !errors.Is(err, ErrNotFound) {
		t.Fatal("exhausted scan failed to retain pause for sibling jobs", err)
	}
	if until, err := r.Cooldown(ctx, "connection", now); err != nil || !until.Equal(s.NotBefore) || !until.After(now) {
		t.Fatal("budget exhaustion removed observed upstream backpressure", until, err)
	}
}

func TestBackpressureRespectsAuthorizationDeadlineAndRejectsMutations(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		ctx := context.Background()
		now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		r := NewMemory()
		scope := Scope{"reader", "connection"}
		backpressureInsert(t, r, scope, Spec{Scan: &ScanSpec{Bucket: "data"}}, now.Add(time.Second), now)
		s := backpressureClaim(t, r, now)
		s = backpressureSave(t, r, s, Checkpoint{Throttle: true}, now)
		if !s.NotBefore.Equal(s.Deadline) {
			t.Fatal("pause exceeded original authorization", s)
		}
		if _, err := r.Claim(ctx, "expired", s.Deadline, time.Second); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		s, err := r.Get(ctx, scope, s.ID)
		if err != nil || s.State != Failed || s.Code != "AuthorizationExpired" {
			t.Fatal("expired scan remained resumable", s, err)
		}
	})
	t.Run("mutation", func(t *testing.T) {
		ctx := context.Background()
		now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		r := NewMemory()
		scope := Scope{"writer", "connection"}
		backpressureInsert(t, r, scope, transferSpec(1), now.Add(time.Hour), now)
		s := backpressureClaim(t, r, now)
		for _, started := range []bool{false, true} {
			if started {
				s = backpressureSave(t, r, s, Checkpoint{Begin: true}, now)
			}
			if _, err := r.Save(ctx, s.ID, s.LeaseID, s.Revision, Checkpoint{Throttle: true}, now); !errors.Is(err, ErrInvalid) {
				t.Fatal("mutation accepted automatic scan retry", err)
			}
			got, err := r.Get(ctx, scope, s.ID)
			if err != nil || !Same(got, s) {
				t.Fatal("invalid throttle changed transfer record", got, err)
			}
		}
	})
}

func TestWaitForCooldownAcceptsOnlyExpiredSafeExecution(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := NewMemory()
	_ = backpressureInsert(t, r, Scope{"reader", "connection"}, Spec{Scan: &ScanSpec{Bucket: "data"}}, now.Add(time.Hour), now)
	s := backpressureClaim(t, r, now)
	s = backpressureSave(t, r, s, Checkpoint{Execute: true}, now)
	for name, mutate := range map[string]func(*Snapshot){
		"queued":     func(s *Snapshot) { s.State = Queued },
		"live-lease": func(s *Snapshot) { s.LeaseUntil = now.Add(3 * time.Second) },
		"mutation":   func(s *Snapshot) { s.InFlight = true },
	} {
		t.Run(name, func(t *testing.T) {
			copy := s.Clone()
			mutate(&copy)
			if _, err := WaitForCooldown(copy, now.Add(4*time.Second), now.Add(2*time.Second)); !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid recovery accepted", err)
			}
		})
	}
	for _, until := range []time.Time{now.Add(time.Second), now.Add(2 * time.Second), now.Add(2*time.Second + MaxThrottleDelay + time.Microsecond)} {
		if _, err := WaitForCooldown(s, until, now.Add(2*time.Second)); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid pause accepted", until, err)
		}
	}
	waiting, err := WaitForCooldown(s, now.Add(4*time.Second), now.Add(2*time.Second))
	if err != nil || waiting.State != Queued || waiting.LeaseID != "" || !Valid(waiting) || s.Timing.ExecutionSince.IsZero() {
		t.Fatal("safe recovery failed or aliased source snapshot", waiting, err)
	}
}

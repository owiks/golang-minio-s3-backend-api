package operations

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTimingSeparatesAdmissionQueueAndExecutionAcrossRetry(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := NewMemory()
	scope := Scope{"actor", "connection"}
	s, err := r.Insert(ctx, scope, transferSpec(2), now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	assert := func(s Snapshot, at time.Time, phase, reason string, wait, execution time.Duration) {
		t.Helper()
		waiting, active, known := s.Durations(at)
		p, why := s.Phase()
		if !Valid(s) || !known || waiting != wait || active != execution || p != phase || why != reason {
			t.Fatalf("phase=%s/%s time=%s/%s valid=%v snapshot=%+v timing=%+v", p, why, waiting, active, Valid(s), s, s.Timing)
		}
	}
	assert(s, now.Add(2*time.Second), "waiting", "scheduler", 2*time.Second, 0)
	s, err = r.Claim(ctx, "worker", now.Add(2*time.Second), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	assert(s, now.Add(3*time.Second), "waiting", "admission", 3*time.Second, 0)
	s, err = r.Save(ctx, s.ID, s.LeaseID, s.Revision, Checkpoint{Execute: true}, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	assert(s, now.Add(4*time.Second), "preparing", "", 3*time.Second, time.Second)
	s, err = r.Save(ctx, s.ID, s.LeaseID, s.Revision, Checkpoint{Begin: true}, now.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	assert(s, now.Add(5*time.Second), "copying", "", 3*time.Second, 2*time.Second)
	s, err = r.Save(ctx, s.ID, s.LeaseID, s.Revision, Checkpoint{Transfer: &TransferOutcome{Copied: true}}, now.Add(6*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	assert(s, now.Add(7*time.Second), "waiting", "scheduler", 4*time.Second, 3*time.Second)
	s, _ = r.Claim(ctx, "worker", now.Add(8*time.Second), time.Minute)
	s, _ = r.Save(ctx, s.ID, s.LeaseID, s.Revision, Checkpoint{Execute: true}, now.Add(9*time.Second))
	s, err = r.Save(ctx, s.ID, s.LeaseID, s.Revision, Checkpoint{FailCode: "AuthorizationDenied"}, now.Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	assert(s, now.Add(30*time.Second), Failed, "", 6*time.Second, 4*time.Second)
	s, err = r.Retry(ctx, scope, s.ID, s.Revision, now.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	assert(s, now.Add(31*time.Second), "waiting", "scheduler", 7*time.Second, 4*time.Second)
	s, _ = r.Claim(ctx, "worker", now.Add(32*time.Second), time.Minute)
	s, _ = r.Save(ctx, s.ID, s.LeaseID, s.Revision, Checkpoint{Execute: true}, now.Add(32*time.Second))
	s, _ = r.Save(ctx, s.ID, s.LeaseID, s.Revision, Checkpoint{Begin: true}, now.Add(32*time.Second))
	s, err = r.Save(ctx, s.ID, s.LeaseID, s.Revision, Checkpoint{Transfer: &TransferOutcome{Copied: true}}, now.Add(34*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	assert(s, now.Add(time.Hour), Completed, "", 8*time.Second, 6*time.Second)
	if !s.Timing.StartedAt.Equal(now.Add(3*time.Second)) || !s.Timing.FinishedAt.Equal(now.Add(34*time.Second)) {
		t.Fatal("execution timestamps lost", s.Timing)
	}
}

func TestTimingLeaseRecoveryCapsExecutionAndPreservesUnknownFence(t *testing.T) {
	r := NewMemory()
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	scope := Scope{"actor", "connection"}
	if _, err := r.Insert(ctx, scope, transferSpec(1), now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	s, err := r.Claim(ctx, "worker", now.Add(time.Second), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s, _ = r.Save(ctx, s.ID, s.LeaseID, s.Revision, Checkpoint{Execute: true}, now.Add(2*time.Second))
	s, _ = r.Save(ctx, s.ID, s.LeaseID, s.Revision, Checkpoint{Begin: true}, now.Add(2*time.Second))
	if _, err := r.Claim(ctx, "recovery", now.Add(2*time.Minute), time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatal("uncertain operation was reclaimed", err)
	}
	s, err = r.Get(ctx, scope, s.ID)
	waiting, execution, known := s.Durations(now.Add(time.Hour))
	if err != nil || !known || !Valid(s) || s.State != Unknown || waiting != 61*time.Second || execution != 59*time.Second {
		t.Fatal(s.State, waiting, execution, s.Timing, err)
	}
}

func TestLegacyAndTimingSnapshotIsolation(t *testing.T) {
	r := NewMemory()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	scope := Scope{"actor", "connection"}
	s, _ := New(scope, transferSpec(1), now.Add(time.Hour), now)
	s.Timing = nil
	if err := r.Import(ctx, []Snapshot{s}); err != nil {
		t.Fatal("legacy backup rejected", err)
	}
	s, _ = r.Claim(ctx, "worker", now, time.Minute)
	s, _ = r.Save(ctx, s.ID, s.LeaseID, s.Revision, Checkpoint{Begin: true}, now)
	s, _ = r.Save(ctx, s.ID, s.LeaseID, s.Revision, Checkpoint{Transfer: &TransferOutcome{Copied: true}}, now)
	if _, _, known := s.Durations(now); known || !Valid(s) {
		t.Fatal("legacy execution accounting invented", s)
	}
	fresh, _ := r.Insert(ctx, scope, transferSpec(1), now.Add(time.Hour), now)
	fresh.Timing.WaitingSince = now.Add(time.Hour)
	got, _ := r.Get(ctx, scope, fresh.ID)
	if !got.Timing.WaitingSince.Equal(now) {
		t.Fatal("timing aliases caller")
	}
	got.Timing.ExecutionUS = -1
	if Valid(got) {
		t.Fatal("invalid timing accepted")
	}
}

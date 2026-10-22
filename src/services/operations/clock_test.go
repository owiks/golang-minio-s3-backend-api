package operations

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOperationTransitionsSurviveBackwardClock(t *testing.T) {
	for _, scenario := range []string{"transfer", "scan", "cancel", "retry", "lease", "throttle"} {
		t.Run(scenario, func(t *testing.T) {
			r := NewMemory()
			t.Cleanup(func() { _ = r.Close() })
			checkBackwardClock(t, r, scenario)
		})
	}
}

func checkBackwardClock(t *testing.T, r Repository, scenario string) {
	t.Helper()
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(seconds int) time.Time { return base.Add(time.Duration(seconds) * time.Second) }
	scope := Scope{ActorID: "actor", ConnectionID: "connection"}
	spec := Spec{Scan: &ScanSpec{Bucket: "source"}}
	if scenario == "transfer" || scenario == "lease" {
		spec = transferSpec(1)
		spec.Transfers[0].Move = true
	}
	s, err := r.Insert(ctx, scope, spec, at(3600), base)
	if err != nil {
		t.Fatal(err)
	}
	valid := func(previous, next Snapshot, err error) Snapshot {
		t.Helper()
		if err != nil || !Valid(next) || next.UpdatedAt.Before(previous.UpdatedAt) {
			t.Fatalf("clock rollback corrupted transition: state=%s timing=%+v updated=%v previous=%v err=%v", next.State, next.Timing, next.UpdatedAt, previous.UpdatedAt, err)
		}
		return next
	}
	claim := func(previous Snapshot, when time.Time) Snapshot {
		t.Helper()
		next, err := r.Claim(ctx, "worker", when, time.Minute)
		return valid(previous, next, err)
	}
	save := func(previous Snapshot, c Checkpoint, when time.Time) Snapshot {
		t.Helper()
		next, err := r.Save(ctx, previous.ID, previous.LeaseID, previous.Revision, c, when)
		return valid(previous, next, err)
	}
	s = claim(s, at(1))
	s = save(s, Checkpoint{Execute: true}, at(2))
	if waiting, execution, known := s.Durations(at(1)); !known || waiting != 2*time.Second || execution != 0 {
		t.Fatal("backward read changed measured durations", waiting, execution, known)
	}
	switch scenario {
	case "transfer":
		s = save(s, Checkpoint{Begin: true}, at(1))
		s = save(s, Checkpoint{Transfer: &TransferOutcome{Copied: true, Moved: true}}, base)
		if s.State != Completed || len(s.Items) != 1 || !s.Items[0].Moved {
			t.Fatal("known successful mutation lost its outcome", s.State, s.Items)
		}
	case "scan":
		s = save(s, Checkpoint{Page: &ScanPage{Objects: 2, Bytes: 3}}, at(1))
		if s.State != Completed || s.Scan.Objects != 2 || s.Scan.Bytes != 3 || s.Scan.Pages != 1 {
			t.Fatal("successful page lost its checkpoint", s.State, s.Scan)
		}
	case "throttle":
		s = save(s, Checkpoint{Throttle: true}, at(1))
		if s.State != Queued || s.ThrottleTotal != 1 || !s.NotBefore.Equal(at(4)) {
			t.Fatal("backward clock moved bounded pause behind observed transition", s.State, s.NotBefore)
		}
		if _, err = r.Claim(ctx, "worker", at(3), time.Minute); !errors.Is(err, ErrNotFound) {
			t.Fatal("backward worker bypassed pause", err)
		}
		s = claim(s, at(4))
		s = save(s, Checkpoint{Execute: true}, at(3))
		s = save(s, Checkpoint{Page: &ScanPage{Objects: 1}}, at(2))
		if s.State != Completed || s.Scan.Objects != 1 || s.ThrottleTotal != 1 || !s.NotBefore.IsZero() {
			t.Fatal("safe resumed page lost its result after clock rollback", s.State, s.Scan)
		}
	case "cancel":
		next, err := r.Cancel(ctx, scope, s.ID, s.Revision, at(1))
		s = valid(s, next, err)
		if !s.CancelRequested {
			t.Fatal("backward cancellation was lost")
		}
		s = save(s, Checkpoint{FailCode: "OperationInterrupted"}, base)
		if s.State != Cancelled {
			t.Fatal("cancelled execution was not finalized", s.State)
		}
	case "retry":
		s = save(s, Checkpoint{FailCode: "ScanFailed"}, at(3))
		next, err := r.Retry(ctx, scope, s.ID, s.Revision, at(2))
		s = valid(s, next, err)
		s = claim(s, at(1))
		s = save(s, Checkpoint{Execute: true}, base)
		s = save(s, Checkpoint{Page: &ScanPage{Objects: 1}}, base)
		if s.State != Completed || s.Scan.Objects != 1 {
			t.Fatal("explicit safe retry lost its result", s.State, s.Scan)
		}
	case "lease":
		s = save(s, Checkpoint{Begin: true}, at(2))
		leaseUntil := s.LeaseUntil
		if err := r.Renew(ctx, s.ID, s.LeaseID, at(1), time.Second); err != nil {
			t.Fatal("backward renewal rejected current lease", err)
		}
		next, err := r.Get(ctx, scope, s.ID)
		s = valid(s, next, err)
		if s.LeaseUntil.Before(leaseUntil) {
			t.Fatal("backward renewal shortened lease")
		}
		if err = r.Renew(ctx, s.ID, s.LeaseID, s.LeaseUntil, time.Minute); !errors.Is(err, ErrConflict) {
			t.Fatal("expired lease was renewed", err)
		}
		if _, err = r.Save(ctx, s.ID, s.LeaseID, s.Revision, Checkpoint{Transfer: &TransferOutcome{Copied: true, Moved: true}}, s.LeaseUntil); !errors.Is(err, ErrConflict) {
			t.Fatal("expired mutation accepted late outcome", err)
		}
		if _, err = r.Claim(ctx, "recovery", s.LeaseUntil, time.Minute); !errors.Is(err, ErrNotFound) {
			t.Fatal("expired mutation was re-executed", err)
		}
		next, err = r.Get(ctx, scope, s.ID)
		s = valid(s, next, err)
		if s.State != Unknown || s.Retryable() {
			t.Fatal("expired mutation lost uncertainty fence", s.State)
		}
	}
}

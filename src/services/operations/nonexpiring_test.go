package operations

import (
	"context"
	"testing"
	"time"
)

func TestPermanentAuthorizationSurvivesQueueRestoreAndRetainsStepBudget(t *testing.T) {
	ctx := context.Background()
	r := NewMemory()
	now := time.Now()
	scope := Scope{"permanent", "connection"}
	queued, err := r.Insert(ctx, scope, transferSpec(1), time.Time{}, now)
	if err != nil || !Valid(queued) || !queued.Deadline.IsZero() {
		t.Fatal("permanent manifest rejected", err)
	}
	backup, err := r.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	restored := NewMemory()
	if err = restored.Import(ctx, backup); err != nil {
		t.Fatal("permanent authorization lost on restore", err)
	}
	called := false
	executor := &executorStub{transferHook: func(step context.Context) {
		called = true
		deadline, ok := step.Deadline()
		if !ok || !deadline.After(time.Now()) || time.Until(deadline) > MaxStepDuration {
			t.Error("permanent credential bypassed per-step execution budget")
		}
	}}
	if err = (Worker{Repository: restored, Executor: executor}).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := restored.Get(ctx, scope, queued.ID)
	if err != nil || !called || result.State != Completed || !Valid(result) || result.ExpiresAt.IsZero() {
		t.Fatal("permanent operation did not complete with finite result retention", result.State, err)
	}
}

func TestPermanentAuthorizationSupportsThrottleAndManualRetry(t *testing.T) {
	ctx := context.Background()
	r := NewMemory()
	now := normalized(time.Now())
	scope := Scope{"permanent", "connection"}
	s, err := r.Insert(ctx, scope, Spec{Scan: &ScanSpec{Bucket: "bucket"}}, time.Time{}, now)
	if err != nil {
		t.Fatal(err)
	}
	s, err = r.Claim(ctx, "worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s, err = r.Save(ctx, s.ID, s.LeaseID, s.Revision, Checkpoint{Throttle: true}, now)
	if err != nil || s.State != Queued || !s.NotBefore.After(now) || !Valid(s) {
		t.Fatal("permanent job lost throttle fence", s, err)
	}
	now = s.NotBefore
	s, err = r.Claim(ctx, "worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s, err = r.Save(ctx, s.ID, s.LeaseID, s.Revision, Checkpoint{FailCode: "ScanFailed"}, now)
	if err != nil || s.State != Failed {
		t.Fatal(s, err)
	}
	s, err = r.Retry(ctx, scope, s.ID, s.Revision, now.Add(time.Second))
	if err != nil || s.State != Queued || !Valid(s) {
		t.Fatal("manual retry rejected permanent authorization", s, err)
	}
}

func TestPermanentAuthorizationDoesNotRepeatInterruptedMutation(t *testing.T) {
	ctx := context.Background()
	r := NewMemory()
	now := time.Now()
	scope := Scope{"permanent", "connection"}
	s, err := r.Insert(ctx, scope, transferSpec(1), time.Time{}, now)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := r.Claim(ctx, "worker", now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Save(ctx, s.ID, claimed.LeaseID, claimed.Revision, Checkpoint{Begin: true}, now); err != nil {
		t.Fatal(err)
	}
	_, _ = r.Claim(ctx, "replacement", now.Add(time.Second), time.Second)
	got, err := r.Get(ctx, scope, s.ID)
	if err != nil || got.State != Unknown || !Valid(got) {
		t.Fatal("interrupted permanent transfer was not fenced", got, err)
	}
}

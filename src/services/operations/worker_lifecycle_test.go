package operations

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExecutorPanicIsFencedAndWorkerPoolContinues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := NewMemory()
	now := time.Now()
	scope := Scope{"a", "c"}
	first, _ := r.Insert(ctx, scope, transferSpec(1), now.Add(time.Hour), now)
	second, _ := r.Insert(ctx, scope, transferSpec(1), now.Add(time.Hour), now.Add(time.Microsecond))
	calls := 0
	var reports []string
	e := &executorStub{transferHook: func(step context.Context) {
		calls++
		deadline, ok := step.Deadline()
		if !ok || time.Until(deadline) > MaxStepDuration {
			t.Error("missing maximum step budget")
		}
		if calls == 1 {
			panic("private upstream credentials")
		}
		cancel()
	}}
	w := Worker{Repository: r, Executor: e, Concurrency: 1, Poll: 10 * time.Millisecond, Report: func(code string) { reports = append(reports, code) }}
	done := make(chan struct{})
	go func() { defer close(done); w.Run(ctx) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not join after panic")
	}
	s, err := r.Get(context.Background(), scope, first.ID)
	if err != nil || s.State != Unknown || len(s.Items) != 1 || s.Items[0].Code != "OutcomeUnknown" {
		t.Fatal(s, err)
	}
	s, err = r.Get(context.Background(), scope, second.ID)
	if err != nil || s.State != Completed || calls != 2 {
		t.Fatal("pool did not continue", s, err, calls)
	}
	if _, err = r.Retry(context.Background(), scope, first.ID, first.Revision, time.Now()); !errors.Is(err, ErrConflict) {
		t.Fatal("panic mutation retry allowed", err)
	}
	if len(reports) != 1 || reports[0] != "OperationsUnavailable" {
		t.Fatal("unsafe or missing panic report", reports)
	}
}

type claimErrorRepository struct {
	Repository
	calls  int
	err    error
	cancel context.CancelFunc
}

func (r *claimErrorRepository) Claim(context.Context, string, time.Time, time.Duration) (Snapshot, error) {
	r.calls++
	if r.calls == 3 {
		r.cancel()
	}
	return Snapshot{}, r.err
}
func TestWorkerReportsStorageFailureOnceAndSkipsIdlePolling(t *testing.T) {
	for _, testErr := range []error{ErrUnavailable, ErrNotFound, errors.New("private upstream credentials")} {
		ctx, cancel := context.WithCancel(context.Background())
		r := &claimErrorRepository{Repository: NewMemory(), err: testErr, cancel: cancel}
		var reports []string
		Worker{Repository: r, Executor: &executorStub{}, Concurrency: 1, Poll: 10 * time.Millisecond, Report: func(code string) { reports = append(reports, code) }}.Run(ctx)
		cancel()
		if errors.Is(testErr, ErrNotFound) {
			if len(reports) != 0 {
				t.Fatal("idle polling logged", reports)
			}
		} else {
			expected := "OperationsUnavailable"
			if !errors.Is(testErr, ErrUnavailable) {
				expected = "OperationStepFailed"
			}
			if len(reports) != 1 || reports[0] != expected {
				t.Fatal("unthrottled or unsafe worker report", reports)
			}
		}
	}
}
func TestPreflightPanicFailsBeforeEffects(t *testing.T) {
	r := NewMemory()
	ctx := context.Background()
	now := time.Now()
	scope := Scope{"a", "c"}
	s, _ := r.Insert(ctx, scope, transferSpec(1), now.Add(time.Hour), now)
	e := &executorStub{authorizePanic: true}
	w := Worker{Repository: r, Executor: e}
	if err := w.RunOnce(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	s, _ = r.Get(ctx, scope, s.ID)
	if s.State != Failed || s.InFlight || s.NextIndex != 0 || len(s.Items) != 0 || e.transferCalls != 0 {
		t.Fatal(s)
	}
}
func TestActiveCancellationInterruptsExecutorAndJoins(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := NewMemory()
	now := time.Now()
	scope := Scope{"a", "c"}
	s, _ := r.Insert(ctx, scope, transferSpec(2), now.Add(time.Hour), now)
	entered := make(chan struct{})
	interrupted := make(chan struct{})
	e := &executorStub{transferHook: func(step context.Context) { close(entered); <-step.Done(); close(interrupted) }, transferErr: context.Canceled}
	w := Worker{Repository: r, Executor: e, Concurrency: 1, Lease: time.Second}
	done := make(chan struct{})
	go func() { defer close(done); w.Run(ctx) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("executor did not start")
	}
	s, _ = r.Get(ctx, scope, s.ID)
	if _, err := r.Cancel(ctx, scope, s.ID, s.Revision, time.Now()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-interrupted:
	case <-time.After(3 * time.Second):
		t.Fatal("active cancellation did not interrupt executor")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled worker did not join")
	}
	s, _ = r.Get(context.Background(), scope, s.ID)
	if s.State != Unknown || s.NextIndex != 0 || e.transferCalls != 1 {
		t.Fatal(s, e.transferCalls)
	}
}
func TestStepTimeoutInterruptsMutationWithoutRetry(t *testing.T) {
	r := NewMemory()
	ctx := context.Background()
	now := time.Now()
	scope := Scope{"a", "c"}
	s, _ := r.Insert(ctx, scope, transferSpec(1), now.Add(24*time.Hour), now)
	e := &executorStub{transferHook: func(step context.Context) { <-step.Done() }, transferErr: context.DeadlineExceeded}
	w := Worker{Repository: r, Executor: e, StepTimeout: 20 * time.Millisecond}
	done := make(chan error, 1)
	go func() { done <- w.RunOnce(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("step ignored execution budget")
	}
	s, _ = r.Get(ctx, scope, s.ID)
	if s.State != Unknown || e.transferCalls != 1 {
		t.Fatal(s)
	}
}

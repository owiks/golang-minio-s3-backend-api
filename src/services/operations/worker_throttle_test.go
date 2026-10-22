package operations

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type throttleExecutor struct {
	executorStub
	scan func(context.Context, Snapshot, ScanSpec, string) (ScanPage, error)
}

func (e *throttleExecutor) ScanPage(ctx context.Context, s Snapshot, spec ScanSpec, token string) (ScanPage, error) {
	return e.scan(ctx, s, spec, token)
}

func TestWorkerDefersOnlyFailedScanPageAndReleasesOtherConnections(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Microsecond)
	var stamp atomic.Int64
	stamp.Store(base.UnixMicro())
	now := func() time.Time { return time.UnixMicro(stamp.Load()).UTC() }
	r := NewMemory()
	scope := Scope{ActorID: "reader", ConnectionID: "limited"}
	job, err := r.Insert(ctx, scope, Spec{Scan: &ScanSpec{Bucket: "bucket"}}, base.Add(time.Hour), base)
	if err != nil {
		t.Fatal(err)
	}
	var tokens []string
	e := &throttleExecutor{scan: func(_ context.Context, s Snapshot, _ ScanSpec, token string) (ScanPage, error) {
		if s.Scope.ConnectionID == "other" {
			return ScanPage{Objects: 1, Bytes: 2}, nil
		}
		tokens = append(tokens, token)
		switch len(tokens) {
		case 1:
			return ScanPage{Objects: 3, Bytes: 9, NextToken: "cursor"}, nil
		case 2:
			return ScanPage{}, ErrThrottled
		case 3:
			return ScanPage{Objects: 2, Bytes: 4}, nil
		default:
			t.Error("unexpected repeated page")
			return ScanPage{}, errors.New("unexpected page")
		}
	}}
	monitor := NewMonitor()
	w := Worker{Repository: r, Executor: e, Now: now, Monitor: monitor}
	if err = w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	firstSuccess := monitor.Snapshot().LastSuccessfulStepAt
	stamp.Add(1)
	if err = w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	paused, err := r.Get(ctx, scope, job.ID)
	if err != nil || !Valid(paused) || paused.State != Queued || paused.Scan.Token != "cursor" || paused.Scan.Pages != 1 || paused.Scan.Objects != 3 || paused.Scan.Bytes != 9 || paused.ThrottleTotal != 1 || !paused.NotBefore.Equal(now().Add(2*time.Second)) {
		t.Fatal("throttled page advanced or lost durable checkpoint", paused, err)
	}
	observed := monitor.Snapshot()
	if observed.Active != 0 || observed.LastFailureCode != "S3Throttled" || observed.LastSuccessfulStepAt == nil || firstSuccess == nil || !observed.LastSuccessfulStepAt.Equal(*firstSuccess) {
		t.Fatal("throttling was counted as successful work", observed)
	}
	backup, err := r.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r = NewMemory()
	if err = r.Import(ctx, backup); err != nil {
		t.Fatal(err)
	}
	w.Repository = r
	otherScope := Scope{ActorID: "other", ConnectionID: "other"}
	other, err := r.Insert(ctx, otherScope, Spec{Scan: &ScanSpec{Bucket: "bucket"}}, base.Add(time.Hour), now())
	if err != nil {
		t.Fatal(err)
	}
	if err = w.RunOnce(ctx); err != nil {
		t.Fatal("paused connection blocked another connection", err)
	}
	completed, err := r.Get(ctx, otherScope, other.ID)
	if err != nil || completed.State != Completed || len(tokens) != 2 {
		t.Fatal("worker did not release the paused connection", completed.State, tokens, err)
	}
	stamp.Store(paused.NotBefore.Add(-time.Microsecond).UnixMicro())
	if err = w.RunOnce(ctx); !errors.Is(err, ErrNotFound) || len(tokens) != 2 {
		t.Fatal("page resumed before pause elapsed", err, tokens)
	}
	stamp.Store(paused.NotBefore.UnixMicro())
	if err = w.RunOnce(ctx); err != nil {
		t.Fatal("page did not resume at exact deadline", err)
	}
	completed, err = r.Get(ctx, scope, job.ID)
	if err != nil || !Valid(completed) || completed.State != Completed || completed.Scan.Pages != 2 || completed.Scan.Objects != 5 || completed.Scan.Bytes != 13 || completed.ThrottleTotal != 1 || completed.ThrottleConsecutive != 0 || !completed.NotBefore.IsZero() || len(tokens) != 3 || tokens[0] != "" || tokens[1] != "cursor" || tokens[2] != "cursor" {
		t.Fatal("safe retry repeated a committed page or lost its totals", completed, tokens, err)
	}
}

func TestWorkerDoesNotAutomaticallyRepeatOtherErrorsOrMutations(t *testing.T) {
	for _, scenario := range []string{"scan-generic", "mutation-throttled"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			now := time.Now().UTC().Truncate(time.Microsecond)
			r := NewMemory()
			scope := Scope{ActorID: "actor", ConnectionID: "connection"}
			spec := Spec{Scan: &ScanSpec{Bucket: "bucket"}}
			if scenario == "mutation-throttled" {
				spec = transferSpec(1)
			}
			job, err := r.Insert(ctx, scope, spec, now.Add(time.Hour), now)
			if err != nil {
				t.Fatal(err)
			}
			scanCalls := 0
			e := &throttleExecutor{executorStub: executorStub{transferErr: ErrThrottled}, scan: func(context.Context, Snapshot, ScanSpec, string) (ScanPage, error) {
				scanCalls++
				return ScanPage{}, errors.New("private upstream failure")
			}}
			w := Worker{Repository: r, Executor: e, Now: func() time.Time { return now }}
			if err = w.RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
			got, err := r.Get(ctx, scope, job.ID)
			if err != nil || !Valid(got) || !got.NotBefore.IsZero() || got.ThrottleTotal != 0 {
				t.Fatal("non-repeatable failure gained a retry fence", got, err)
			}
			if scenario == "mutation-throttled" {
				if got.State != Unknown || got.Retryable() || e.transferCalls != 1 || scanCalls != 0 {
					t.Fatal("mutation throttling lost unknown outcome", got, e.transferCalls, scanCalls)
				}
			} else if got.State != Failed || got.Code != "ScanFailed" || scanCalls != 1 || e.transferCalls != 0 {
				t.Fatal("generic scan failure became automatic retry", got, scanCalls)
			}
			if err = w.RunOnce(ctx); !errors.Is(err, ErrNotFound) || e.transferCalls > 1 || scanCalls > 1 {
				t.Fatal("terminal work automatically repeated", err, e.transferCalls, scanCalls)
			}
		})
	}
}

func TestWorkerThrottleKeepsConnectionFenceAfterCancellationOrExpiry(t *testing.T) {
	for _, scenario := range []string{"cancel", "deadline"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			base := time.Now().UTC().Truncate(time.Microsecond)
			var stamp atomic.Int64
			stamp.Store(base.UnixMicro())
			now := func() time.Time { return time.UnixMicro(stamp.Load()).UTC() }
			r := NewMemory()
			scope := Scope{ActorID: "reader", ConnectionID: "limited"}
			job, err := r.Insert(ctx, scope, Spec{Scan: &ScanSpec{Bucket: "bucket"}}, base.Add(30*time.Second), base)
			if err != nil {
				t.Fatal(err)
			}
			e := &throttleExecutor{scan: func(_ context.Context, s Snapshot, _ ScanSpec, _ string) (ScanPage, error) {
				if scenario == "cancel" {
					if _, err := r.Cancel(ctx, scope, s.ID, s.Revision, now()); err != nil {
						t.Error(err)
					}
				} else {
					stamp.Store(s.Deadline.UnixMicro())
				}
				return ScanPage{}, ErrThrottled
			}}
			w := Worker{Repository: r, Executor: e, Now: now}
			if err = w.RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
			got, err := r.Get(ctx, scope, job.ID)
			want := Cancelled
			if scenario == "deadline" {
				want = Failed
			}
			if err != nil || !Valid(got) || got.State != want || got.Scan.Pages != 0 || !got.NotBefore.After(now()) {
				t.Fatal("stopped scan lost observed backpressure", got, err)
			}
			until, err := r.Cooldown(ctx, scope.ConnectionID, now())
			if err != nil || !until.Equal(got.NotBefore) {
				t.Fatal("connection fence absent", until, err)
			}
		})
	}
}

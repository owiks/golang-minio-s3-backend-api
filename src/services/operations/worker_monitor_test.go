package operations

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWorkerMonitorRecordsOnlySuccessfulDurableWork(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	repo := NewMemory()
	monitor := NewMonitor()
	monitor.Started(1, now)
	_, err := repo.Insert(ctx, Scope{"actor", "connection"}, transferSpec(1), now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	executor := &executorStub{transferHook: func(context.Context) {
		if monitor.Snapshot().Active != 1 {
			t.Error("admitted execution missing")
		}
	}}
	worker := Worker{Repository: repo, Executor: executor, Monitor: monitor}
	if err = worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got := monitor.Snapshot()
	if got.Active != 0 || got.LastSuccessfulStepAt == nil || got.LastCompletedAt == nil || got.LastFailureCode != "" {
		t.Fatal(got)
	}
	_, err = repo.Insert(ctx, Scope{"actor", "connection"}, transferSpec(1), now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	executor.transferErr = errors.New("private upstream error")
	if err = worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	next := monitor.Snapshot()
	if !next.LastSuccessfulStepAt.Equal(*got.LastSuccessfulStepAt) || next.LastFailureCode != "OutcomeUnknown" || next.Active != 0 {
		t.Fatal(next)
	}
	if err = worker.RunOnce(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if monitor.Snapshot().LastFailureCode != "OutcomeUnknown" {
		t.Fatal("idle poll changed failure")
	}
}

func TestWorkerMonitorDoesNotCountRejectedAdmission(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	repo := NewMemory()
	_, err := repo.Insert(ctx, Scope{"actor", "connection"}, transferSpec(1), now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	monitor := NewMonitor()
	err = (Worker{Repository: repo, Executor: &executorStub{}, Admission: &deniedAdmission{}, Monitor: monitor}).RunOnce(ctx)
	got := monitor.Snapshot()
	if !errors.Is(err, ErrLimit) || got.Active != 0 || got.LastSuccessfulStepAt != nil || got.LastFailureCode != "OperationsBusy" {
		t.Fatal(got, err)
	}
}

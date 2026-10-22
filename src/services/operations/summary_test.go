package operations

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSummaryAggregatesWithoutMutatingOrExposingRecords(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	m := NewMemory()
	first, err := m.Insert(ctx, Scope{"private-actor", "private-connection"}, Spec{Scan: &ScanSpec{Bucket: "private-bucket", Prefix: "private-prefix/"}}, now.Add(time.Hour), now.Add(-10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := m.Claim(ctx, "private-worker", now.Add(-5*time.Second), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Insert(ctx, Scope{"another", "connection"}, transferSpec(1), now.Add(time.Hour), now.Add(-3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.Summary(ctx, now)
	if err != nil || got.Scope != "store" || got.Queued != 1 || got.Running != 1 || got.ExpiredLeases != 1 || got.OldestQueuedAgeSec != 3 {
		t.Fatal(got, err)
	}
	unchanged, err := m.Get(ctx, first.Scope, first.ID)
	if err != nil || unchanged.Revision != claimed.Revision || unchanged.State != Running {
		t.Fatal("summary performed lease recovery", err)
	}
	raw, _ := json.Marshal(got)
	for _, private := range []string{"private", first.ID, claimed.LeaseID, "actorID", "leaseID", "token", "spec"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private data in summary", private)
		}
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Summary(ctx, now); !errors.Is(err, ErrUnavailable) {
		t.Fatal("closed store", err)
	}
}

func TestSummaryIgnoresExpiredTerminalRecords(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	m := NewMemory()
	s, err := m.Insert(ctx, Scope{"actor", "connection"}, transferSpec(1), now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	s, err = m.Cancel(ctx, s.Scope, s.ID, s.Revision, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.Summary(ctx, s.ExpiresAt)
	if err != nil || got.Cancelled != 0 {
		t.Fatal("retention summary", got, err)
	}
	if _, err = m.Get(ctx, s.Scope, s.ID); err != nil {
		t.Fatal("summary pruned storage", err)
	}
}

func TestMonitorDistinguishesProgressFromSuccessfulWork(t *testing.T) {
	now := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	m := NewMonitor()
	m.Started(2, now)
	m.Polled(now)
	done := m.BeginStep()
	if v := m.Snapshot(); !v.Running || v.Active != 1 || v.LastSuccessfulStepAt != nil {
		t.Fatal(v)
	}
	m.Checkpoint(Snapshot{State: Failed, Code: "ScanFailed"}, Checkpoint{FailCode: "ScanFailed"}, now)
	if v := m.Snapshot(); v.LastSuccessfulStepAt != nil || v.LastFailureCode != "ScanFailed" {
		t.Fatal(v)
	}
	m.Checkpoint(Snapshot{State: Completed}, Checkpoint{Page: &ScanPage{}}, now.Add(time.Second))
	v := m.Snapshot()
	if v.LastSuccessfulStepAt == nil || v.LastCompletedAt == nil || !v.LastCompletedAt.Equal(now.Add(time.Second)) {
		t.Fatal(v)
	}
	*v.StartedAt = time.Time{}
	if m.Snapshot().StartedAt.IsZero() {
		t.Fatal("timestamp aliases mutable monitor state")
	}
	done()
	done()
	m.Stopped(now.Add(2 * time.Second))
	if v = m.Snapshot(); v.Running || v.Active != 0 {
		t.Fatal(v)
	}
	if NewMonitor().Snapshot().LastFailureAt != nil {
		t.Fatal("new process inherited observations")
	}
}

func TestMonitorConcurrentSnapshotsAndSteps(t *testing.T) {
	m := NewMonitor()
	now := time.Now()
	m.Started(16, now)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				end := m.BeginStep()
				m.Polled(now)
				m.Checkpoint(Snapshot{State: Completed}, Checkpoint{Page: &ScanPage{}}, now)
				m.Failure("OperationStoreUnavailable", now)
				_ = m.Snapshot()
				end()
			}
		}()
	}
	wg.Wait()
	if m.Snapshot().Active != 0 {
		t.Fatal("active execution leaked")
	}
}

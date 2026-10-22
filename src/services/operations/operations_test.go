package operations

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
)

func transferSpec(n int) Spec {
	p := Spec{}
	for range n {
		p.Transfers = append(p.Transfers, transfers.FileTransferRequest{SourceBucket: "source", SourceKey: "file", SourceRevision: "checked", DestinationBucket: "target", DestinationKey: "copy", DestinationRevision: "absent"})
	}
	return p
}

func TestManifestRequiresDestinationSnapshotBeforeQueueing(t *testing.T) {
	spec := transferSpec(1)
	spec.Transfers[0].DestinationRevision = ""
	r := NewMemory()
	now := time.Now()
	if _, err := r.Insert(context.Background(), Scope{"a", "c"}, spec, now.Add(time.Hour), now); !errors.Is(err, ErrInvalid) {
		t.Fatal("unfenced destination accepted", err)
	}
	all, err := r.List(context.Background(), Scope{"a", "c"})
	if err != nil || len(all) != 0 {
		t.Fatal("invalid manifest reached queue", all, err)
	}
}
func TestLeaseLossNeverRepeatsMutation(t *testing.T) {
	ctx := context.Background()
	r := NewMemory()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	scope := Scope{"actor", "connection"}
	s, err := r.Insert(ctx, scope, transferSpec(2), now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := r.Claim(ctx, "first", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Claim(ctx, "second", now, time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatal("duplicate claim", err)
	}
	started, err := r.Save(ctx, s.ID, claimed.LeaseID, claimed.Revision, Checkpoint{Begin: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Claim(ctx, "second", now.Add(time.Minute), time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatal("uncertain mutation requeued", err)
	}
	final, err := r.Get(ctx, scope, s.ID)
	if err != nil || final.State != Unknown || len(final.Items) != 1 || final.Items[0].State != Unknown {
		t.Fatal(final, err)
	}
	if _, err = r.Retry(ctx, scope, s.ID, final.Revision, now.Add(time.Minute)); !errors.Is(err, ErrConflict) {
		t.Fatal("uncertain retry", err)
	}
	if _, err = r.Save(ctx, s.ID, claimed.LeaseID, started.Revision, Checkpoint{Transfer: &TransferOutcome{Copied: true}}, now.Add(time.Minute)); !errors.Is(err, ErrConflict) {
		t.Fatal("stale worker completion", err)
	}
}
func TestSnapshotIsolationCancellationAndScope(t *testing.T) {
	ctx := context.Background()
	r := NewMemory()
	now := time.Now()
	scope := Scope{"actor", "connection"}
	p := transferSpec(2)
	s, err := r.Insert(ctx, scope, p, now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	p.Transfers[0].SourceKey = "modified"
	s.Spec.Transfers[0].SourceKey = "modifiedagain"
	got, _ := r.Get(ctx, scope, s.ID)
	if got.Spec.Transfers[0].SourceKey != "file" {
		t.Fatal("manifest aliases caller")
	}
	if _, err = r.Get(ctx, Scope{"actor", "other"}, s.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("scope leak", err)
	}
	claimed, _ := r.Claim(ctx, "worker", now, time.Minute)
	started, _ := r.Save(ctx, s.ID, claimed.LeaseID, claimed.Revision, Checkpoint{Begin: true}, now)
	cancelled, err := r.Cancel(ctx, scope, s.ID, started.Revision, now)
	if err != nil || !cancelled.CancelRequested {
		t.Fatal(cancelled, err)
	}
	if _, err = r.Save(ctx, s.ID, started.LeaseID, started.Revision, Checkpoint{Transfer: &TransferOutcome{Copied: true}}, now); !errors.Is(err, ErrConflict) {
		t.Fatal("stale revision accepted", err)
	}
	final, err := r.Save(ctx, s.ID, cancelled.LeaseID, cancelled.Revision, Checkpoint{Transfer: &TransferOutcome{Copied: true}}, now)
	if err != nil || final.State != Cancelled || final.NextIndex != 1 || !final.Items[0].Copied {
		t.Fatal(final, err)
	}
}
func TestScanCheckpointCyclesAndRetry(t *testing.T) {
	ctx := context.Background()
	r := NewMemory()
	now := time.Now()
	scope := Scope{"actor", "connection"}
	s, err := r.Insert(ctx, scope, Spec{Scan: &ScanSpec{Bucket: "bucket", Prefix: "folder/"}}, now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	claimed, _ := r.Claim(ctx, "worker", now, time.Minute)
	s, err = r.Save(ctx, s.ID, claimed.LeaseID, claimed.Revision, Checkpoint{Page: &ScanPage{Objects: 2, Bytes: 100, NextToken: "next"}}, now)
	if err != nil || s.State != Queued {
		t.Fatal(s, err)
	}
	claimed, _ = r.Claim(ctx, "worker", now, time.Minute)
	if _, err = r.Save(ctx, s.ID, claimed.LeaseID, claimed.Revision, Checkpoint{Page: &ScanPage{Objects: 2, Bytes: 100, NextToken: "next"}}, now); !errors.Is(err, ErrInvalid) {
		t.Fatal("cycle accepted", err)
	}
	s, err = r.Save(ctx, s.ID, claimed.LeaseID, claimed.Revision, Checkpoint{FailCode: "ScanFailed"}, now)
	if err != nil {
		t.Fatal(err)
	}
	s, err = r.Retry(ctx, scope, s.ID, s.Revision, now)
	if err != nil || s.Scan.Token != "next" || s.Scan.Objects != 2 || s.Scan.Bytes != 100 {
		t.Fatal("checkpoint lost", s, err)
	}
	claimed, _ = r.Claim(ctx, "worker", now, time.Minute)
	s, err = r.Save(ctx, s.ID, claimed.LeaseID, claimed.Revision, Checkpoint{Page: &ScanPage{Objects: 1, Bytes: 50}}, now)
	if err != nil || s.State != Completed || s.Scan.Objects != 3 || s.Scan.Bytes != 150 {
		t.Fatal(s, err)
	}
}
func TestActorSchedulingYieldsBetweenObjects(t *testing.T) {
	r := NewMemory()
	ctx := context.Background()
	now := time.Now()
	first, _ := r.Insert(ctx, Scope{"a", "c"}, transferSpec(2), now.Add(time.Hour), now)
	_, _ = r.Insert(ctx, Scope{"b", "c"}, transferSpec(1), now.Add(time.Hour), now.Add(time.Microsecond))
	s, _ := r.Claim(ctx, "w", now, time.Minute)
	if s.ID != first.ID {
		t.Fatal("oldest actor not first")
	}
	s, _ = r.Save(ctx, s.ID, s.LeaseID, s.Revision, Checkpoint{Begin: true}, now)
	s, _ = r.Save(ctx, s.ID, s.LeaseID, s.Revision, Checkpoint{Transfer: &TransferOutcome{Copied: true}}, now)
	if s.State != Queued {
		t.Fatal("long manifest held worker")
	}
	s, _ = r.Claim(ctx, "w", now.Add(time.Second), time.Minute)
	if s.Scope.ActorID != "b" {
		t.Fatal("second actor starved", s.Scope)
	}
}

type executorStub struct {
	authorizeErr   error
	transferCalls  int
	transferErr    error
	after          func()
	authorizePanic bool
	transferHook   func(context.Context)
}

func (e *executorStub) Authorize(context.Context, Snapshot) error {
	if e.authorizePanic {
		panic("private authorization detail")
	}
	return e.authorizeErr
}
func (e *executorStub) Transfer(ctx context.Context, _ Snapshot, _ transfers.FileTransferRequest) (TransferOutcome, error) {
	e.transferCalls++
	if e.transferHook != nil {
		e.transferHook(ctx)
	}
	if e.after != nil {
		e.after()
	}
	return TransferOutcome{Copied: true}, e.transferErr
}
func (e *executorStub) ScanPage(context.Context, Snapshot, ScanSpec, string) (ScanPage, error) {
	return ScanPage{Objects: 1, Bytes: 2}, nil
}
func TestWorkerRechecksAuthorizationAndDoesNotRetryUnknown(t *testing.T) {
	r := NewMemory()
	ctx := context.Background()
	now := time.Now()
	scope := Scope{"a", "c"}
	s, _ := r.Insert(ctx, scope, transferSpec(2), now.Add(time.Hour), now)
	e := &executorStub{}
	w := Worker{Repository: r, Executor: e}
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	e.authorizeErr = errors.New("revoked")
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	s, _ = r.Get(ctx, scope, s.ID)
	if e.transferCalls != 1 || s.State != Failed || s.NextIndex != 1 {
		t.Fatal(s, e.transferCalls)
	}
	e.authorizeErr = nil
	e.transferErr = errors.New("response lost")
	if _, err := r.Retry(ctx, scope, s.ID, s.Revision, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	s, _ = r.Get(ctx, scope, s.ID)
	if s.State != Unknown || e.transferCalls != 2 {
		t.Fatal(s, e.transferCalls)
	}
	if err := w.RunOnce(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatal("unknown rerun", err)
	}
}
func TestWorkerCancellationSavesKnownResultAndStopsNext(t *testing.T) {
	r := NewMemory()
	ctx := context.Background()
	now := time.Now()
	scope := Scope{"a", "c"}
	s, _ := r.Insert(ctx, scope, transferSpec(2), now.Add(time.Hour), now)
	e := &executorStub{after: func() {
		v, _ := r.Get(ctx, scope, s.ID)
		if _, err := r.Cancel(ctx, scope, s.ID, v.Revision, time.Now()); err != nil {
			t.Error(err)
		}
	}}
	w := Worker{Repository: r, Executor: e}
	if err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	s, _ = r.Get(ctx, scope, s.ID)
	if s.State != Cancelled || s.NextIndex != 1 || e.transferCalls != 1 {
		t.Fatal(s)
	}
}

type deniedAdmission struct{ scope Scope }

func (a *deniedAdmission) Acquire(_ context.Context, scope Scope) (func(), error) {
	a.scope = scope
	return nil, ErrLimit
}
func TestAdmissionYieldsWithoutMutation(t *testing.T) {
	r := NewMemory()
	ctx := context.Background()
	now := time.Now()
	scope := Scope{"a", "c"}
	s, _ := r.Insert(ctx, scope, transferSpec(1), now.Add(time.Hour), now)
	e := &executorStub{}
	a := &deniedAdmission{}
	w := Worker{Repository: r, Executor: e, Admission: a}
	if err := w.RunOnce(ctx); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	s, _ = r.Get(ctx, scope, s.ID)
	if s.State != Queued || s.InFlight || e.transferCalls != 0 || a.scope != scope {
		t.Fatal(s, a.scope, e.transferCalls)
	}
}
func TestTerminalHistoryDoesNotBecomeDailyQuota(t *testing.T) {
	r := NewMemory()
	ctx := context.Background()
	now := time.Now()
	scope := Scope{"a", "c"}
	var oldest string
	for i := 0; i < MaxPerActor+2; i++ {
		s, err := r.Insert(ctx, scope, transferSpec(1), now.Add(time.Hour), now.Add(time.Duration(i)*time.Microsecond))
		if err != nil {
			t.Fatal("completed jobs exhausted capacity", i, err)
		}
		if i == 0 {
			oldest = s.ID
		}
		if _, err = r.Cancel(ctx, scope, s.ID, s.Revision, now.Add(time.Duration(i)*time.Microsecond)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Get(ctx, scope, oldest); !errors.Is(err, ErrNotFound) {
		t.Fatal("oldest terminal not evicted", err)
	}
	all, _ := r.List(ctx, scope)
	if len(all) != MaxPerActor {
		t.Fatal(len(all))
	}
}
func TestOfflineImportIsRepeatableAndCannotOverwrite(t *testing.T) {
	ctx := context.Background()
	r := NewMemory()
	now := time.Now()
	scope := Scope{"a", "c"}
	s, _ := r.Insert(ctx, scope, transferSpec(1), now.Add(time.Hour), now)
	export, err := r.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	target := NewMemory()
	for range 2 {
		if err = target.Import(ctx, export); err != nil {
			t.Fatal(err)
		}
	}
	export[0].Spec.Transfers[0].SourceKey = "changed"
	if err = target.Import(ctx, export); !errors.Is(err, ErrConflict) {
		t.Fatal("import overwrote target", err)
	}
	got, _ := target.Get(ctx, scope, s.ID)
	if got.Spec.Transfers[0].SourceKey != "file" {
		t.Fatal("import aliases data")
	}
}

func TestOfflineBackupRejectsAggregateOver64MiB(t *testing.T) {
	ctx := context.Background()
	r := NewMemory()
	now := time.Now()
	spec := transferSpec(200)
	for i := range spec.Transfers {
		spec.Transfers[i].SourceKey = strings.Repeat("s", 1024)
		spec.Transfers[i].DestinationKey = strings.Repeat("d", 1024)
	}
	if !spec.Valid() {
		t.Fatal("fixture manifest must be individually valid")
	}
	records := make([]Snapshot, 0, 150)
	for i := range 150 {
		s, err := r.Insert(ctx, Scope{ActorID: "actor" + strconv.Itoa(i/MaxPerActor), ConnectionID: "c"}, spec, now.Add(time.Hour), now)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, s)
	}
	if snapshots, err := r.Export(ctx); !errors.Is(err, ErrLimit) || snapshots != nil {
		t.Fatal("oversized export not bounded", len(snapshots), err)
	}
	if ValidBackup(records) {
		t.Fatal("oversized import catalog accepted")
	}
	if err := NewMemory().Import(ctx, records); !errors.Is(err, ErrInvalid) {
		t.Fatal("oversized import accepted", err)
	}
}

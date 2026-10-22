package operations

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
)

// Executor resolves the current authenticated connection on every step. It must
// recheck grants, resource restrictions, expiration and operator policy before
// upstream I/O. It must never cache credentials or authorization in a manifest.
type Executor interface {
	Authorize(context.Context, Snapshot) error
	Transfer(context.Context, Snapshot, transfers.FileTransferRequest) (TransferOutcome, error)
	ScanPage(context.Context, Snapshot, ScanSpec, string) (ScanPage, error)
}
type Admission interface {
	Acquire(context.Context, Scope) (func(), error)
}
type Worker struct {
	Repository  Repository
	Executor    Executor
	Admission   Admission
	Concurrency int
	Lease       time.Duration
	Poll        time.Duration
	Now         func() time.Time
	// StepTimeout may shorten the execution budget; it cannot exceed 30 minutes.
	StepTimeout time.Duration
	// Report receives only stable classifications, never raw upstream/panic text.
	Report func(string)
}

const MaxStepDuration = 30 * time.Minute

func (w Worker) clock() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}
func (w Worker) lease() time.Duration {
	if w.Lease >= time.Second && w.Lease <= time.Hour {
		return w.Lease
	}
	return time.Minute
}

// Run joins all worker and lease-renewal goroutines before returning. Jobs are
// yielded after each object/page so actors with short jobs are not starved by a
// long manifest. Shutdown never starts a replacement for an in-flight mutation.
func (w Worker) Run(ctx context.Context) {
	n := w.Concurrency
	if n < 1 {
		n = 2
	}
	if n > 16 {
		n = 16
	}
	poll := w.Poll
	if poll < 10*time.Millisecond {
		poll = 500 * time.Millisecond
	}
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lastCode := ""
			var lastReport time.Time
			for ctx.Err() == nil {
				err := w.RunOnce(ctx)
				if err == nil {
					lastCode = ""
					continue
				}
				if code := failureCode(err); code != "" && (code != lastCode || time.Since(lastReport) >= time.Minute) {
					w.report(code)
					lastCode = code
					lastReport = time.Now()
				}
				timer := time.NewTimer(poll)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}()
	}
	wg.Wait()
}

func failureCode(err error) string {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, context.Canceled):
		return ""
	case errors.Is(err, ErrUnavailable):
		return "OperationsUnavailable"
	case errors.Is(err, ErrInvalid):
		return "OperationsInvalid"
	case errors.Is(err, ErrConflict):
		return "OperationConflict"
	case errors.Is(err, ErrLimit):
		return "OperationsBusy"
	case errors.Is(err, context.DeadlineExceeded):
		return "OperationTimeout"
	default:
		return "OperationStepFailed"
	}
}
func (w Worker) report(code string) {
	if w.Report == nil {
		return
	}
	defer func() { _ = recover() }()
	w.Report(code)
}

func (w Worker) RunOnce(ctx context.Context) (resultErr error) {
	var s Snapshot
	// A defective executor is isolated to its claimed job. Persist a neutral
	// result without logging panic values, which may contain credentials.
	defer func() {
		if recover() != nil {
			resultErr = ErrUnavailable
			if s.ID != "" {
				if err := w.recoverClaim(s); err != nil {
					resultErr = err
				}
			}
		}
	}()
	if w.Repository == nil || w.Executor == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	s, err = w.Repository.Claim(ctx, securetoken.New(), w.clock(), w.lease())
	if err != nil {
		return err
	}
	if w.Admission != nil {
		release, admitErr := w.Admission.Acquire(ctx, s.Scope)
		if admitErr != nil {
			if yieldErr := w.finish(s, Checkpoint{Yield: true}); yieldErr != nil {
				return yieldErr
			}
			return admitErr
		}
		defer release()
	}
	s, err = w.save(s, Checkpoint{Execute: true})
	if err != nil || s.Terminal() {
		return err
	}
	budget := w.StepTimeout
	if budget <= 0 || budget > MaxStepDuration {
		budget = MaxStepDuration
	}
	deadline := time.Now().Add(budget)
	if s.Deadline.Before(deadline) {
		deadline = s.Deadline
	}
	stepCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	stopHeartbeat := w.heartbeat(stepCtx, cancel, s)
	defer stopHeartbeat()
	if err = w.Executor.Authorize(stepCtx, s.Clone()); err != nil {
		return w.finish(s, Checkpoint{FailCode: "AuthorizationDenied"})
	}
	if err = stepCtx.Err(); err != nil {
		return w.finish(s, Checkpoint{FailCode: "OperationInterrupted"})
	}
	if s.Kind == "scan" {
		page, readErr := w.Executor.ScanPage(stepCtx, s.Clone(), *s.Spec.Scan, s.Scan.Token)
		if readErr != nil {
			return w.finish(s, Checkpoint{FailCode: "ScanFailed"})
		}
		err = w.finish(s, Checkpoint{Page: &page})
		if errors.Is(err, ErrInvalid) {
			return w.finish(s, Checkpoint{FailCode: "InvalidScanPage"})
		}
		return err
	}
	s, err = w.save(s, Checkpoint{Begin: true})
	if err != nil {
		return err
	}
	if s.Terminal() {
		return nil
	}
	outcome, runErr := w.Executor.Transfer(stepCtx, s.Clone(), s.Spec.Transfers[s.NextIndex])
	if runErr != nil {
		outcome = TransferOutcome{Uncertain: true, Code: "OutcomeUnknown"}
	}
	return w.finish(s, Checkpoint{Transfer: &outcome})
}

func (w Worker) recoverClaim(s Snapshot) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrUnavailable
		}
	}()
	return w.finish(s, Checkpoint{FailCode: "OperationInterrupted"})
}

func (w Worker) finish(s Snapshot, c Checkpoint) error { _, err := w.save(s, c); return err }
func (w Worker) save(s Snapshot, c Checkpoint) (Snapshot, error) {
	// Keep the outcome durable even when the request/server execution context
	// has been cancelled. This bounded cleanup never retries upstream work.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for range 3 {
		next, err := w.Repository.Save(ctx, s.ID, s.LeaseID, s.Revision, c, w.clock())
		if !errors.Is(err, ErrConflict) {
			return next, err
		}
		current, getErr := w.Repository.Get(ctx, s.Scope, s.ID)
		if getErr != nil {
			return Snapshot{}, getErr
		}
		if current.State != Running || current.LeaseID != s.LeaseID {
			return Snapshot{}, ErrConflict
		}
		s = current
	}
	return Snapshot{}, ErrConflict
}
func (w Worker) heartbeat(ctx context.Context, cancel context.CancelFunc, s Snapshot) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if recover() != nil {
				cancel()
			}
		}()
		interval := min(w.lease()/3, time.Second)
		timer := time.NewTicker(interval)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-timer.C:
				renewCtx, release := context.WithTimeout(ctx, interval)
				current, err := w.Repository.Get(renewCtx, s.Scope, s.ID)
				if err == nil && (current.State != Running || current.LeaseID != s.LeaseID || current.CancelRequested) {
					release()
					cancel()
					return
				}
				if err == nil {
					err = w.Repository.Renew(renewCtx, s.ID, s.LeaseID, w.clock(), w.lease())
				}
				release()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	return func() { close(stop); <-done }
}

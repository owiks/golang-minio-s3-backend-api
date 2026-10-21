package runtime_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

// Done is evaluated by the lock's select after it has registered the waiter.
// This provides a barrier without sleeps or access to private session state.
type observedLockContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *observedLockContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestSessionAccessLockCancelsWaitingAdapterWithoutUnlockingOwner(t *testing.T) {
	client := runtime.NewClients(testsession.New(t, "http://fixture.example"))
	owner, err := client.LockAccess(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer owner()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiter := &observedLockContext{Context: ctx, waiting: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		release, err := client.LockAccess(waiter)
		if release != nil {
			release()
		}
		done <- err
	}()
	select {
	case <-waiter.waiting:
	case <-time.After(time.Second):
		t.Fatal("waiter did not reach the lock")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("waiter acquired a held lock or lost cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled waiter still depends on owner release")
	}
	assertAccessLockHeld(t, client)
	owner()
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	release, err := client.LockAccess(ctx)
	if err != nil {
		t.Fatal("cancelled waiter retained the lock", err)
	}
	release()
}

func assertAccessLockHeld(t *testing.T, client *runtime.Clients) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	release, err := client.LockAccess(ctx)
	if release != nil {
		release()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("active owner's lock was released by a different request", err)
	}
}

func TestSessionAccessLockRejectsExpiredContextEvenWhenFree(t *testing.T) {
	for _, kind := range []string{"cancelled", "deadline"} {
		t.Run(kind, func(t *testing.T) {
			client := runtime.NewClients(testsession.New(t, "http://fixture.example"))
			ctx, cancel := context.WithCancel(context.Background())
			want := context.Canceled
			if kind == "deadline" {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				want = context.DeadlineExceeded
			}
			cancel()
			for range 50 { // Both select branches are ready on an uncontended lock.
				release, err := client.LockAccess(ctx)
				if release != nil {
					release()
					t.Fatal("failed acquisition returned an unlock callback")
				}
				if !errors.Is(err, want) {
					t.Fatal("expired context acquired the lock", err)
				}
			}
			release, err := client.LockAccess(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			release()
		})
	}
}

func TestSessionAccessLockRetainsOwnershipAfterContextCancellationAndRepeatedRelease(t *testing.T) {
	client := runtime.NewClients(testsession.New(t, "http://fixture.example"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first, err := client.LockAccess(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	cancel() // Cancellation of an owner is not permission to overlap its work.
	assertAccessLockHeld(t, client)
	first()
	second, err := client.LockAccess(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer second()
	first() // A late duplicate release must not unlock the new owner.
	assertAccessLockHeld(t, client)
}

func TestSessionAccessLockScopeAndConcurrentAdapters(t *testing.T) {
	session := testsession.New(t, "http://fixture.example")
	one := runtime.NewClients(session)
	owner, err := one.LockAccess(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer owner()
	other := runtime.NewClients(testsession.New(t, "http://fixture.example"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	independent, err := other.LockAccess(ctx)
	if err != nil {
		t.Fatal("independent session inherited another session's lock", err)
	}
	independent()
	owner()
	var jobs sync.WaitGroup
	count := 0 // Intentionally non-atomic: -race verifies mutual exclusion.
	for range 16 {
		jobs.Go(func() {
			adapter := runtime.NewClients(session)
			for range 25 {
				release, err := adapter.LockAccess(context.Background())
				if err != nil {
					t.Error(err)
					return
				}
				count++
				release()
			}
		})
	}
	jobs.Wait()
	if count != 400 {
		t.Fatal("concurrent adapter updates overlapped", count)
	}
}

func TestSessionAccessLockCancellationRacingReleaseDoesNotLeakOwnership(t *testing.T) {
	client := runtime.NewClients(testsession.New(t, "http://fixture.example"))
	for range 100 {
		owner, err := client.LockAccess(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		waiter := &observedLockContext{Context: ctx, waiting: make(chan struct{})}
		done := make(chan error, 1)
		go func() {
			release, err := client.LockAccess(waiter)
			if release != nil {
				release()
			}
			done <- err
		}()
		<-waiter.waiting
		var finish sync.WaitGroup
		finish.Go(cancel)
		finish.Go(owner)
		finish.Wait()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal("unexpected cancellation race result", err)
		}
		ctx, stop := context.WithTimeout(context.Background(), time.Second)
		release, err := client.LockAccess(ctx)
		stop()
		if err != nil {
			t.Fatal("cancellation race leaked a lock token", err)
		}
		release()
	}
}

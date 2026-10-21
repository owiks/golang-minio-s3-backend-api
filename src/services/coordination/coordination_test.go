package coordination

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestLocksSerializeAndReleaseCancelledWaiters(t *testing.T) {
	var locks Locks
	release, err := locks.Acquire(context.Background(), "server/bucket")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := locks.Acquire(ctx, "server/bucket"); err != context.Canceled {
		t.Fatal(err)
	}
	independent, err := locks.Acquire(context.Background(), "other-server/bucket")
	if err != nil {
		t.Fatal(err)
	}
	independent()
	release()
	release()
	var count int
	var jobs sync.WaitGroup
	for range 100 {
		jobs.Go(func() {
			done, err := locks.Acquire(context.Background(), "server/bucket")
			if err != nil {
				t.Error(err)
				return
			}
			count++
			done()
		})
	}
	jobs.Wait()
	if count != 100 || len(locks.entries) != 0 {
		t.Fatalf("lost writes or retained locks: %d %d", count, len(locks.entries))
	}
}
func TestAdmissionFairnessAndIdempotentRelease(t *testing.T) {
	a := NewAdmission(3, 2)
	first, ok := a.Try("one")
	if !ok {
		t.Fatal("first rejected")
	}
	second, _ := a.Try("one")
	if _, ok := a.Try("one"); ok {
		t.Fatal("principal consumed all capacity")
	}
	third, ok := a.Try("two")
	if !ok {
		t.Fatal("unrelated principal blocked")
	}
	if _, ok := a.Try("three"); ok {
		t.Fatal("global limit ignored")
	}
	first()
	first()
	second()
	third()
	if a.Active() != 0 || len(a.principals) != 0 {
		t.Fatal("capacity leaked")
	}
}
func TestAttemptsRefillAndBoundedMemory(t *testing.T) {
	a := NewAttempts(2, 2, time.Second)
	now := time.Unix(100, 0)
	for range 2 {
		if ok, _ := a.Allow("a", now); !ok {
			t.Fatal("burst rejected")
		}
	}
	if ok, wait := a.Allow("a", now); ok || wait != time.Second {
		t.Fatalf("invalid budget %v %v", ok, wait)
	}
	if ok, _ := a.Allow("a", now.Add(time.Second/2)); ok {
		t.Fatal("early refill")
	}
	if ok, _ := a.Allow("a", now.Add(time.Second)); !ok {
		t.Fatal("did not refill")
	}
	if ok, _ := a.Allow("b", now.Add(time.Second)); !ok {
		t.Fatal("second client rejected")
	}
	if ok, _ := a.Allow("c", now.Add(time.Second)); ok {
		t.Fatal("table must fail closed")
	}
	if ok, _ := a.Allow("c", now.Add(4*time.Second)); !ok {
		t.Fatal("expired entries not reclaimed")
	}
}

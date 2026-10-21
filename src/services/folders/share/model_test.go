package foldershare

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
)

type admission struct {
	calls, released int
	owner           string
}

type concurrentAdmission struct{ released atomic.Int32 }

func (a *concurrentAdmission) AcquireTransfer(string) (func(), error) {
	return func() { a.released.Add(1) }, nil
}
func TestBeginReadRacingWithStopReleasesExactlyOnce(t *testing.T) {
	for range 40 {
		grant := New(Config{Expires: time.Now().Add(time.Hour)})
		admission := &concurrentAdmission{}
		var ctx context.Context
		var release func()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); ctx, release, _ = grant.BeginRead(context.Background(), admission) }()
		go func() { defer wg.Done(); grant.Stop() }()
		wg.Wait()
		if ctx != nil {
			if ctx.Err() != context.Canceled {
				t.Fatal("read escaped revoke")
			}
			release()
			release()
			if admission.released.Load() != 1 {
				t.Fatal("slot released more than once")
			}
		}
		if grant.ActiveReads() != 0 {
			t.Fatal("active read retained")
		}
	}
}

func (a *admission) AcquireTransfer(owner string) (func(), error) {
	a.calls++
	a.owner = owner
	return func() { a.released++ }, nil
}

func TestDelegatedReadHasItsOwnLifetimeAndRevocation(t *testing.T) {
	a := &admission{}
	g := New(Config{Owner: "owner", Prefix: "reports/", Expires: time.Now().Add(time.Hour)})
	ctx, release, err := g.BeginRead(context.Background(), a)
	if err != nil || a.owner != "owner" || len(g.active) != 1 {
		t.Fatal("read admission lost", err)
	}
	if deadline, ok := ctx.Deadline(); !ok || !deadline.Equal(g.Expires()) {
		t.Fatal("grant deadline lost")
	}
	if !g.Contains("reports/file") || g.Contains("other/file") || g.Contains("reports/\xff") {
		t.Fatal("grant scope changed")
	}
	g.Stop()
	if ctx.Err() != context.Canceled {
		t.Fatal("revoked read still active")
	}
	release()
	release()
	if len(g.active) != 0 || a.released != 1 {
		t.Fatal("read not released")
	}
	if _, _, err = g.BeginRead(context.Background(), a); err == nil || apperror.Public(err).Code != "FolderShareExpired" || a.calls != 1 {
		t.Fatal("revoked read admitted", err)
	}
	expired := New(Config{Expires: time.Now().Add(-time.Second)})
	if _, _, err = expired.BeginRead(context.Background(), a); err == nil || a.calls != 1 {
		t.Fatal("expired read admitted", err)
	}
}

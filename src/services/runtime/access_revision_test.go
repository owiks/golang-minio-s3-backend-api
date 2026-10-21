package runtime

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestAccessRevisionEvictionNeverReusesTokens(t *testing.T) {
	store := NewAccessRevisions()
	now := time.Now()
	first := store.revision("original", false, now)
	store.revision("original", true, now)
	changed := store.revision("original", false, now)
	if first == changed {
		t.Fatal("invalidation lost")
	}
	for i := 0; i < 10000; i++ {
		store.revision(fmt.Sprint(i), true, now)
	}
	if len(store.revisions) != accessRevisionLimit || store.recent.Len() != accessRevisionLimit {
		t.Fatal("unbounded endpoint history", len(store.revisions))
	}
	fresh := store.revision("original", false, now)
	if fresh == first || fresh == changed {
		t.Fatal("eviction reused stale token")
	}
	if fresh != store.revision("original", false, now) {
		t.Fatal("unchanged endpoint is unstable")
	}
}
func TestAccessRevisionReapTouchAndClose(t *testing.T) {
	store := NewAccessRevisions()
	now := time.Now()
	old := store.revision("old", false, now)
	store.revision("active", false, now)
	active := store.revision("active", false, now.Add(accessRevisionIdle/2))
	store.Reap(now.Add(accessRevisionIdle))
	if len(store.revisions) != 1 || store.revision("active", false, now.Add(accessRevisionIdle)) != active {
		t.Fatal("live endpoint discarded")
	}
	if store.revision("old", false, now.Add(accessRevisionIdle)) == old {
		t.Fatal("expired endpoint reused token")
	}
	store.Close()
	store.Close()
	store.Invalidate("closed")
	_ = store.Revision("closed")
	if len(store.revisions) != 0 || store.recent.Len() != 0 {
		t.Fatal("closed owner retained state")
	}
}
func TestAccessRevisionConcurrentLifecycle(t *testing.T) {
	store := NewAccessRevisions()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				endpoint := fmt.Sprint(n, j)
				store.Invalidate(endpoint)
				store.Revision(endpoint)
				store.Reap(time.Now())
			}
		}(i)
	}
	wg.Wait()
	store.Close()
}

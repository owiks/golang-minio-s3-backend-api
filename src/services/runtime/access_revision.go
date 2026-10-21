package runtime

import (
	"container/list"
	"strconv"
	"sync"
	"time"

	"minio-web/api/src/services/securetoken"
)

const accessRevisionLimit = 4096
const accessRevisionIdle = time.Hour

type accessRevisionEntry struct {
	endpoint string
	sequence uint64
	touched  time.Time
}

type AccessRevisions struct {
	mu        sync.Mutex
	epoch     string
	sequence  uint64
	revisions map[string]*list.Element
	recent    list.List
	closed    bool
}

func NewAccessRevisions() *AccessRevisions {
	return &AccessRevisions{epoch: securetoken.New(), revisions: map[string]*list.Element{}}
}

// Tokens are allocated from an owner-wide sequence, not a per-endpoint counter.
// Evicting/recreating an endpoint can cause an extra refresh, never a stale match.
func (store *AccessRevisions) revision(endpoint string, invalidate bool, now time.Time) string {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed {
		return store.epoch + ":closed"
	}
	element := store.revisions[endpoint]
	if element == nil {
		if len(store.revisions) >= accessRevisionLimit {
			store.remove(store.recent.Front())
		}
		element = store.recent.PushBack(&accessRevisionEntry{endpoint: endpoint})
		store.revisions[endpoint] = element
		invalidate = true
	}
	entry := element.Value.(*accessRevisionEntry)
	if invalidate {
		store.sequence++
		if store.sequence == 0 {
			store.epoch = securetoken.New()
			store.sequence++
		}
		entry.sequence = store.sequence
	}
	entry.touched = now
	store.recent.MoveToBack(element)
	return store.epoch + ":" + strconv.FormatUint(entry.sequence, 10)
}
func (store *AccessRevisions) Revision(endpoint string) string {
	return store.revision(endpoint, false, time.Now())
}
func (store *AccessRevisions) Invalidate(endpoint string) {
	store.revision(endpoint, true, time.Now())
}
func (store *AccessRevisions) remove(element *list.Element) {
	delete(store.revisions, element.Value.(*accessRevisionEntry).endpoint)
	store.recent.Remove(element)
}
func (store *AccessRevisions) Reap(now time.Time) {
	store.mu.Lock()
	defer store.mu.Unlock()
	// Touch times may arrive out of order when concurrent callers acquire the lock.
	for element := store.recent.Front(); element != nil; {
		next := element.Next()
		if now.Sub(element.Value.(*accessRevisionEntry).touched) >= accessRevisionIdle {
			store.remove(element)
		}
		element = next
	}
}
func (store *AccessRevisions) Close() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.closed = true
	clear(store.revisions)
	store.recent.Init()
}
func (app *State) AccessRevision(s *Session) string {
	return app.revisions.Revision(s.internal.String())
}
func (app *State) InvalidateAccess(s *Session) { app.revisions.Invalidate(s.internal.String()) }

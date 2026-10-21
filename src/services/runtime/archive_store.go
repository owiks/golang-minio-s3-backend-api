package runtime

import (
	"context"
	"sync"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	archiveplan "github.com/owiks/golang-minio-s3-backend-api/src/services/archives/plan"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

type ArchiveStore struct {
	mu         sync.Mutex
	plans      map[string]*archiveplan.ArchivePlan
	closed     bool
	hasSession func(string) bool
}

func NewArchiveStore(hasSession func(string) bool) *ArchiveStore {
	return &ArchiveStore{plans: map[string]*archiveplan.ArchivePlan{}, hasSession: hasSession}
}
func (store *ArchiveStore) Get(ticket, owner string) *archiveplan.ArchivePlan {
	store.mu.Lock()
	defer store.mu.Unlock()
	plan := store.plans[ticket]
	if store.closed || plan == nil || !plan.Available(owner, time.Now()) {
		return nil
	}
	return plan
}
func (store *ArchiveStore) Put(ctx context.Context, plan *archiveplan.ArchivePlan) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if store.closed {
		return "", closingError()
	}
	if !store.hasSession(plan.SessionID()) {
		return "", expiredSessionError()
	}
	count, total := 0, 0
	for ticket, old := range store.plans {
		if time.Since(old.Created()) > time.Hour {
			old.Abort()
			delete(store.plans, ticket)
		} else {
			total += old.ItemCount()
			if old.SessionID() == plan.SessionID() {
				count++
			}
		}
	}
	if count >= 8 || len(store.plans) >= 128 || total+plan.ItemCount() > 10000 {
		return "", apperror.New(429, "ArchiveLimit", messages.RuntimeArchiveLimit)
	}
	ticket := securetoken.New()
	store.plans[ticket] = plan
	return ticket, nil
}

// Remove only the plan inspected by the caller, never a replacement.
func (store *ArchiveStore) Forget(ticket string, plan *archiveplan.ArchivePlan) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.plans[ticket] == plan {
		delete(store.plans, ticket)
	}
}
func (store *ArchiveStore) AbortSession(id string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, plan := range store.plans {
		if plan.SessionID() == id {
			plan.Abort()
		}
	}
}
func (store *ArchiveStore) Reap(now time.Time) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for ticket, plan := range store.plans {
		if now.Sub(plan.Created()) > time.Hour || !store.hasSession(plan.SessionID()) {
			plan.Abort()
			delete(store.plans, ticket)
		}
	}
}
func (store *ArchiveStore) Close() {
	store.mu.Lock()
	store.closed = true
	plans := store.plans
	store.plans = map[string]*archiveplan.ArchivePlan{}
	store.mu.Unlock()
	for _, plan := range plans {
		plan.Abort()
	}
}

package runtime

import (
	"context"
	"sync"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
)

type SessionStore struct {
	mu     sync.RWMutex
	items  map[string]*Session
	closed bool
}

func NewSessionStore() *SessionStore { return &SessionStore{items: map[string]*Session{}} }
func (store *SessionStore) Get(id string) *Session {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.items[id]
}
func (store *SessionStore) Has(id string) bool { return store.Get(id) != nil }
func (store *SessionStore) Put(s *Session) error {
	store.mu.Lock()
	if store.closed {
		store.mu.Unlock()
		return closingError()
	}
	old := store.items[s.id]
	// This is an SDK cache, not the durable auth registry. Eviction closes idle
	// sockets only; it never revokes durable credentials or delegated work.
	if old == nil && len(store.items) >= 256 {
		for id, item := range store.items {
			old = item
			delete(store.items, id)
			break
		}
	}
	store.items[s.id] = s
	store.mu.Unlock()
	if old != s && (old == nil || old.sessionCore != s.sessionCore) {
		closeSession(old)
	}
	return nil
}
func closeSession(s *Session) {
	if s != nil && s.sessionCore != nil && s.transport != nil {
		s.transport.CloseIdleConnections()
	}
}
func (store *SessionStore) Delete(id string) {
	closeSession(store.detach(id))
}

// detach revokes ownership before dependent cleanup starts, while keeping the
// connection pool usable until that cleanup finishes.
func (store *SessionStore) detach(id string) *Session {
	store.mu.Lock()
	s := store.items[id]
	delete(store.items, id)
	store.mu.Unlock()
	return s
}
func (store *SessionStore) Reap(now time.Time) {
	store.mu.Lock()
	removed := []*Session{}
	for id, s := range store.items {
		if s.Expired(now) {
			removed = append(removed, s)
			delete(store.items, id)
		}
	}
	store.mu.Unlock()
	for _, s := range removed {
		closeSession(s)
	}
}
func (store *SessionStore) Close() {
	store.mu.Lock()
	store.closed = true
	removed := store.items
	store.items = map[string]*Session{}
	store.mu.Unlock()
	for _, s := range removed {
		closeSession(s)
	}
}

func (app *State) Session(id string) *Session { return app.Sessions.Get(id) }
func (app *State) StoreSession(s *Session) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return app.StoreSessionContext(ctx, s)
}
func (app *State) RemoveSession(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a, _, err := app.Auth.Lookup(ctx, id)
	if err == nil {
		if app.Auth.Logout(ctx, authstate.CredentialGuard(a, false)) != nil {
			return
		}
	}
	app.removeCachedSession(id)
}
func (app *State) removeCachedSession(id string) {
	s := app.Sessions.detach(id)
	defer closeSession(s)
	app.Multipart.AbortSession(id)
	app.Capabilities.RemoveSession(id)
	app.Archives.AbortSession(id)
	app.ZIPs.Reap(time.Now())
}

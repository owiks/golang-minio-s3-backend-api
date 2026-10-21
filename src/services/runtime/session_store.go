package runtime

import (
	"sync"
	"time"

	"minio-web/api/src/services/apperror"
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
	defer store.mu.Unlock()
	if store.closed {
		return closingError()
	}
	if len(store.items) >= 256 {
		return apperror.New(429, "SessionLimit", "Достигнут лимит активных сессий. Закройте старые подключения или попробуйте позже.")
	}
	store.items[s.id] = s
	return nil
}
func closeSession(s *Session) {
	if s != nil && s.transport != nil {
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

func (app *State) Session(id string) *Session    { return app.Sessions.Get(id) }
func (app *State) StoreSession(s *Session) error { return app.Sessions.Put(s) }
func (app *State) RemoveSession(id string) {
	s := app.Sessions.detach(id)
	defer closeSession(s)
	app.Multipart.AbortSession(id)
	app.Capabilities.RemoveSession(id)
	app.Archives.AbortSession(id)
	app.ZIPs.Reap(time.Now())
}

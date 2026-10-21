package runtime

import (
	"sync"
	"time"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	uploadinvite "minio-web/api/src/services/uploads/invite"
)

type UploadInviteStore struct {
	mu      sync.Mutex
	invites map[string]*uploadinvite.UploadInvite
	closed  bool
}

func NewUploadInviteStore() *UploadInviteStore {
	return &UploadInviteStore{invites: map[string]*uploadinvite.UploadInvite{}}
}
func (store *UploadInviteStore) Get(token string) *uploadinvite.UploadInvite {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.invites[token]
}
func (store *UploadInviteStore) List(owner, bucket, prefix string) []uploadinvite.UploadInviteSummary {
	store.mu.Lock()
	defer store.mu.Unlock()
	rows := []uploadinvite.UploadInviteSummary{}
	for _, g := range store.invites {
		summary, active := g.Summary(time.Now())
		if g.Owner() == owner && active && g.Bucket() == bucket && g.Prefix() == prefix {
			rows = append(rows, summary)
		}
	}
	return rows
}
func (store *UploadInviteStore) Add(g *uploadinvite.UploadInvite) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed {
		return closingError()
	}
	store.reapLocked(time.Now())
	if len(store.invites) >= 256 {
		return apperror.New(429, "InviteLimit", messages.RuntimeInviteLimit)
	}
	if _, exists := store.invites[g.Token()]; exists {
		return apperror.New(409, "InviteExists", messages.RuntimeInviteExists)
	}
	store.invites[g.Token()] = g
	return nil
}
func (store *UploadInviteStore) Revoke(owner, token string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	g := store.invites[token]
	if g == nil || g.Owner() != owner {
		return apperror.New(404, "InviteNotFound", messages.RuntimeInviteNotFound)
	}
	delete(store.invites, token)
	g.Stop()
	return nil
}
func (store *UploadInviteStore) reapLocked(now time.Time) {
	for token, g := range store.invites {
		if now.After(g.Expires()) {
			g.Stop()
			delete(store.invites, token)
		}
	}
}
func (store *UploadInviteStore) Reap(now time.Time) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.reapLocked(now)
}
func (store *UploadInviteStore) Close() {
	store.mu.Lock()
	store.closed = true
	grants := store.invites
	store.invites = map[string]*uploadinvite.UploadInvite{}
	store.mu.Unlock()
	for _, g := range grants {
		g.Stop()
	}
}

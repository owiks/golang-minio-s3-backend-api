package runtime

import (
	"sync"
	"time"

	"minio-web/api/src/services/apperror"
	foldershare "minio-web/api/src/services/folders/share"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/securetoken"
)

// Links and download tickets share an owner so revoke and ticket creation are atomic.
// Lock order: store.mu, then the model's private activity lock.
type FolderShareStore struct {
	mu      sync.Mutex
	links   map[string]*foldershare.FolderShare
	tickets map[string]foldershare.FolderFileTicket
	closed  bool
}

func NewFolderShareStore() *FolderShareStore {
	return &FolderShareStore{links: map[string]*foldershare.FolderShare{}, tickets: map[string]foldershare.FolderFileTicket{}}
}
func (store *FolderShareStore) Get(token string) *foldershare.FolderShare {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.links[token]
}
func (store *FolderShareStore) List(owner, bucket, prefix string) []foldershare.FolderShareSummary {
	store.mu.Lock()
	defer store.mu.Unlock()
	rows := []foldershare.FolderShareSummary{}
	for _, g := range store.links {
		summary, active := g.Summary(time.Now())
		if g.Owner() == owner && active && g.Bucket() == bucket && g.Prefix() == prefix {
			rows = append(rows, summary)
		}
	}
	return rows
}
func (store *FolderShareStore) Add(g *foldershare.FolderShare) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed {
		return closingError()
	}
	store.reapLocked(time.Now())
	if len(store.links) >= 256 {
		return apperror.New(429, "ShareLimit", messages.RuntimeShareLimit)
	}
	if _, exists := store.links[g.Token()]; exists {
		return apperror.New(409, "ShareExists", messages.RuntimeInviteExists)
	}
	store.links[g.Token()] = g
	return nil
}
func (store *FolderShareStore) Revoke(owner, token string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	g := store.links[token]
	if g == nil || g.Owner() != owner {
		return apperror.New(404, "ShareNotFound", messages.RuntimeInviteNotFound)
	}
	delete(store.links, token)
	for id, t := range store.tickets {
		if t.Share == g {
			delete(store.tickets, id)
		}
	}
	g.Stop()
	return nil
}
func (store *FolderShareStore) IssueTicket(g *foldershare.FolderShare, key, kind string) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed {
		return "", closingError()
	}
	if store.links[g.Token()] != g {
		return "", apperror.New(410, "FolderShareExpired", messages.FoldersFolderShareExpiredModel)
	}
	ticket, err := g.FileTicket(key, kind, time.Now())
	if err != nil {
		return "", err
	}
	for id, t := range store.tickets {
		if time.Now().After(t.Expires) {
			delete(store.tickets, id)
		}
	}
	if len(store.tickets) >= 512 {
		return "", apperror.New(429, "TicketLimit", messages.RuntimeTicketLimit)
	}
	id := securetoken.New()
	store.tickets[id] = ticket
	return id, nil
}
func (store *FolderShareStore) TakeTicket(id string, consume bool) (foldershare.FolderFileTicket, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	ticket, ok := store.tickets[id]
	if ok && consume && ticket.Mode == "download" {
		delete(store.tickets, id)
	}
	return ticket, ok
}
func (store *FolderShareStore) reapLocked(now time.Time) {
	for token, g := range store.links {
		if now.After(g.Expires()) {
			g.Stop()
			delete(store.links, token)
		}
	}
	for id, t := range store.tickets {
		if now.After(t.Expires) {
			delete(store.tickets, id)
		}
	}
}
func (store *FolderShareStore) Reap(now time.Time) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.reapLocked(now)
}
func (store *FolderShareStore) Close() {
	store.mu.Lock()
	store.closed = true
	links := store.links
	store.links = map[string]*foldershare.FolderShare{}
	store.tickets = map[string]foldershare.FolderFileTicket{}
	store.mu.Unlock()
	for _, g := range links {
		g.Stop()
	}
}

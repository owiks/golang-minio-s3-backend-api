package uploadinvite

import (
	"context"
	"sync"
	"time"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/securetoken"
)

type Config struct {
	Owner, Token, Bucket, Prefix string
	Expires                      time.Time
	MaxSize                      int64
	MaxFiles                     int
	Writer                       WriteClient
}
type UploadInvite struct {
	mu                           sync.Mutex
	owner, token, bucket, prefix string
	expires                      time.Time
	maxSize                      int64
	maxFiles, uploaded           int
	writer                       WriteClient
	active                       map[string]context.CancelFunc
	revoked                      bool
}

func New(c Config) *UploadInvite {
	return &UploadInvite{owner: c.Owner, token: c.Token, bucket: c.Bucket, prefix: c.Prefix, expires: c.Expires, maxSize: c.MaxSize, maxFiles: c.MaxFiles, writer: c.Writer, active: map[string]context.CancelFunc{}}
}
func (g *UploadInvite) Owner() string       { return g.owner }
func (g *UploadInvite) Bucket() string      { return g.bucket }
func (g *UploadInvite) Prefix() string      { return g.prefix }
func (g *UploadInvite) Token() string       { return g.token }
func (g *UploadInvite) Expires() time.Time  { return g.expires }
func (g *UploadInvite) Writer() WriteClient { return g.writer }

type View struct {
	Token    string    `json:"token"`
	Bucket   string    `json:"bucket"`
	Prefix   string    `json:"prefix"`
	Expires  time.Time `json:"expiresAt"`
	MaxSize  int64     `json:"maxSize"`
	MaxFiles int       `json:"maxFiles"`
	Uploaded int       `json:"uploaded"`
}

func (g *UploadInvite) viewLocked() View {
	return View{Token: g.token, Bucket: g.bucket, Prefix: g.prefix, Expires: g.expires, MaxSize: g.maxSize, MaxFiles: g.maxFiles, Uploaded: g.uploaded}
}
func (g *UploadInvite) Snapshot() View { g.mu.Lock(); defer g.mu.Unlock(); return g.viewLocked() }
func (g *UploadInvite) Inspect() (View, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.revoked || time.Now().After(g.expires) {
		return View{}, expired()
	}
	return g.viewLocked(), nil
}
func (g *UploadInvite) Summary(now time.Time) (UploadInviteSummary, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return UploadInviteSummary{Token: g.token, Expires: g.expires, MaxSize: g.maxSize, MaxFiles: g.maxFiles, Uploaded: g.uploaded}, !g.revoked && now.Before(g.expires)
}
func (g *UploadInvite) Stop() {
	g.mu.Lock()
	g.revoked = true
	active := make([]context.CancelFunc, 0, len(g.active))
	for _, cancel := range g.active {
		active = append(active, cancel)
	}
	g.mu.Unlock()
	for _, cancel := range active {
		cancel()
	}
}
func expired() error {
	return apperror.New(410, "InviteExpired", "Ссылка недействительна или истекла.")
}

// A reservation is finished exactly once. Successful completion updates the
// count and removes the in-flight slot in the same critical section.
type WriteLease struct {
	invite *UploadInvite
	id     string
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func (g *UploadInvite) BeginWrite(parent context.Context) (*WriteLease, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.revoked || time.Now().After(g.expires) {
		return nil, expired()
	}
	if g.uploaded+len(g.active) >= g.maxFiles {
		return nil, apperror.New(409, "InviteLimit", "Лимит файлов исчерпан или ссылка отозвана.")
	}
	id := securetoken.New()
	ctx, cancel := context.WithDeadline(parent, g.expires)
	g.active[id] = cancel
	return &WriteLease{invite: g, id: id, ctx: ctx, cancel: cancel}, nil
}
func (l *WriteLease) Context() context.Context { return l.ctx }
func (l *WriteLease) finish(success bool) {
	l.once.Do(func() {
		g := l.invite
		g.mu.Lock()
		delete(g.active, l.id)
		if success {
			g.uploaded++
		}
		g.mu.Unlock()
		l.cancel()
	})
}
func (l *WriteLease) Commit() { l.finish(true) }
func (l *WriteLease) Close()  { l.finish(false) }

type UploadInviteSummary struct {
	Token    string    `json:"token"`
	Expires  time.Time `json:"expiresAt"`
	MaxSize  int64     `json:"maxSize"`
	MaxFiles int       `json:"maxFiles"`
	Uploaded int       `json:"uploaded"`
}

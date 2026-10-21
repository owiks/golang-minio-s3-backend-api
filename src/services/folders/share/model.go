package foldershare

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/coordination"
	"minio-web/api/src/services/securetoken"
)

type Config struct {
	Owner, Token, Bucket, Prefix string
	Expires                      time.Time
	Reader                       ReadClient
}

// Identity and capability are immutable after construction. Activity is private.
type FolderShare struct {
	mu                           sync.Mutex
	owner, token, bucket, prefix string
	expires                      time.Time
	reader                       ReadClient
	revoked                      bool
	active                       map[string]context.CancelFunc
}

func New(c Config) *FolderShare {
	return &FolderShare{owner: c.Owner, token: c.Token, bucket: c.Bucket, prefix: c.Prefix, expires: c.Expires, reader: c.Reader, active: map[string]context.CancelFunc{}}
}
func (g *FolderShare) Owner() string      { return g.owner }
func (g *FolderShare) Token() string      { return g.token }
func (g *FolderShare) Bucket() string     { return g.bucket }
func (g *FolderShare) Prefix() string     { return g.prefix }
func (g *FolderShare) Expires() time.Time { return g.expires }
func (g *FolderShare) Reader() ReadClient { return g.reader }
func (g *FolderShare) ActiveReads() int   { g.mu.Lock(); defer g.mu.Unlock(); return len(g.active) }

type View struct {
	Token   string    `json:"token"`
	Bucket  string    `json:"bucket"`
	Prefix  string    `json:"prefix"`
	Expires time.Time `json:"expiresAt"`
}

func (g *FolderShare) View() View {
	return View{Token: g.token, Bucket: g.bucket, Prefix: g.prefix, Expires: g.expires}
}
func (g *FolderShare) Summary(now time.Time) (FolderShareSummary, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return FolderShareSummary{Token: g.token, Expires: g.expires, Bucket: g.bucket, Prefix: g.prefix}, !g.revoked && now.Before(g.expires)
}
func (g *FolderShare) Stop() {
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
func (g *FolderShare) Contains(key string) bool {
	return key != "" && len(key) <= 1024 && utf8.ValidString(key) && strings.HasPrefix(key, g.prefix)
}
func (g *FolderShare) BeginRead(parent context.Context, admission coordination.TransferAdmission) (context.Context, func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.revoked || time.Now().After(g.expires) {
		return nil, nil, expired()
	}
	release, err := admission.AcquireTransfer(g.owner)
	if err != nil {
		return nil, nil, err
	}
	id := securetoken.New()
	ctx, cancel := context.WithDeadline(parent, g.expires)
	g.active[id] = cancel
	var once sync.Once
	return ctx, func() { once.Do(func() { cancel(); g.mu.Lock(); delete(g.active, id); g.mu.Unlock(); release() }) }, nil
}
func expired() error {
	return apperror.New(410, "FolderShareExpired", "Ссылка отозвана или срок действия истёк.")
}

// The store serializes issuing this value with link revocation; no lock escapes.
func (g *FolderShare) FileTicket(key, kind string, now time.Time) (FolderFileTicket, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.revoked || now.After(g.expires) {
		return FolderFileTicket{}, expired()
	}
	if !g.Contains(key) {
		return FolderFileTicket{}, apperror.New(403, "FolderScopeDenied", "Файл недоступен по этой ссылке.")
	}
	if kind != "download" && kind != "media" {
		return FolderFileTicket{}, apperror.New(400, "InvalidTicket", "Неверный вид ссылки.")
	}
	expires := g.expires
	if limit := now.Add(5 * time.Minute); kind == "download" && limit.Before(expires) {
		expires = limit
	}
	return FolderFileTicket{Share: g, Key: key, Mode: kind, Expires: expires}, nil
}

type FolderFileTicket struct {
	Share     *FolderShare `json:"-"`
	Key, Mode string       `json:"-"`
	Expires   time.Time    `json:"-"`
}
type FolderShareSummary struct {
	Token   string    `json:"token"`
	Expires time.Time `json:"expiresAt"`
	Bucket  string    `json:"bucket"`
	Prefix  string    `json:"prefix"`
}

// Package authstate owns durable connections, credentials and access rules.
package authstate

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"slices"
	"strings"
	"time"
)

type Resource struct {
	Bucket string `json:"bucket"`
	Prefix string `json:"prefix,omitempty"`
}
type Policy struct {
	Scopes    []string   `json:"scopes"`
	Resources []Resource `json:"resources,omitempty"`
}

var scopes = []string{"objects:read", "objects:write", "objects:delete", "buckets:read", "buckets:manage", "iam:read", "iam:manage", "server:read", "server:manage", "access:manage"}

func FullPolicy() Policy { return Policy{Scopes: slices.Clone(scopes)} }
func (p Policy) Clone() Policy {
	p.Scopes = slices.Clone(p.Scopes)
	p.Resources = slices.Clone(p.Resources)
	return p
}
func (p Policy) Valid() bool {
	if len(p.Resources) > 0 && (p.Allows("iam:manage") || p.Allows("server:manage")) {
		return false
	}
	if len(p.Scopes) == 0 || len(p.Scopes) > len(scopes) || len(p.Resources) > 128 {
		return false
	}
	seen := map[string]bool{}
	for _, s := range p.Scopes {
		if !slices.Contains(scopes, s) || seen[s] {
			return false
		}
		seen[s] = true
	}
	for _, r := range p.Resources {
		if r.Bucket == "" || len(r.Bucket) > 63 || strings.ContainsAny(r.Bucket, "/\\\x00") || len(r.Prefix) > 1024 || strings.ContainsRune(r.Prefix, 0) {
			return false
		}
	}
	return true
}
func (p Policy) Allows(scope string) bool { return slices.Contains(p.Scopes, scope) }
func (p Policy) AllowsObject(bucket, key string) bool {
	if len(p.Resources) == 0 {
		return true
	}
	for _, r := range p.Resources {
		if r.Bucket == bucket && strings.HasPrefix(key, r.Prefix) {
			return true
		}
	}
	return false
}
func (p Policy) AllowsBucket(bucket string) bool {
	if len(p.Resources) == 0 {
		return true
	}
	for _, r := range p.Resources {
		if r.Bucket == bucket && r.Prefix == "" {
			return true
		}
	}
	return false
}
func (p Policy) ListsBucket(bucket string) bool {
	if len(p.Resources) == 0 {
		return true
	}
	for _, r := range p.Resources {
		if r.Bucket == bucket {
			return true
		}
	}
	return false
}
func (p Policy) Contains(child Policy) bool {
	for _, s := range child.Scopes {
		if !p.Allows(s) {
			return false
		}
	}
	if len(p.Resources) == 0 {
		return true
	}
	if len(child.Resources) == 0 {
		return false
	}
	for _, r := range child.Resources {
		if !p.AllowsObject(r.Bucket, r.Prefix) {
			return false
		}
	}
	return true
}
func Owner(endpoint, actor string) string { return Digest(endpoint + "\x00" + actor) }
func Digest(value string) string          { s := sha256.Sum256([]byte(value)); return hex.EncodeToString(s[:]) }
func Match(value, digest string) bool {
	if value == "" || len(digest) != 64 {
		return false
	}
	actual := Digest(value)
	return subtle.ConstantTimeCompare([]byte(actual), []byte(digest)) == 1
}

type Ciphertext struct {
	KeyID string `json:"keyID"`
	Data  []byte `json:"data"`
}
type ConnectionData struct {
	ID            string     `json:"id"`
	Owner         string     `json:"owner"`
	Endpoint      string     `json:"endpoint"`
	Region        string     `json:"region"`
	Actor         string     `json:"actor"`
	Revision      int64      `json:"revision"`
	SecretVersion int64      `json:"secretVersion"`
	Active        bool       `json:"active"`
	CreatedAt     time.Time  `json:"createdAt"`
	Cipher        Ciphertext `json:"cipher"`
}
type Connection struct{ data ConnectionData }

func NewConnection(d ConnectionData) Connection {
	d.Cipher.Data = slices.Clone(d.Cipher.Data)
	return Connection{data: d}
}
func (c Connection) Snapshot() ConnectionData {
	d := c.data
	d.Cipher.Data = slices.Clone(d.Cipher.Data)
	return d
}
func (c Connection) AAD() []byte {
	return []byte(c.data.ID + "\x00" + c.data.Owner + "\x00" + formatVersion(c.data.SecretVersion))
}
func (c Connection) View() ConnectionView {
	d := c.data
	return ConnectionView{ID: d.ID, Endpoint: d.Endpoint, Region: d.Region, Active: d.Active, Revision: d.Revision, CreatedAt: d.CreatedAt}
}

type ConnectionView struct {
	ID        string    `json:"id"`
	Endpoint  string    `json:"endpoint"`
	Region    string    `json:"region"`
	Active    bool      `json:"active"`
	Revision  int64     `json:"revision"`
	CreatedAt time.Time `json:"createdAt"`
}

type AccessData struct {
	ID             string    `json:"id"`
	ConnectionID   string    `json:"connectionID"`
	ParentID       string    `json:"parentID,omitempty"`
	Renewable      bool      `json:"renewable,omitempty"`
	Owner          string    `json:"owner"`
	Name           string    `json:"name"`
	Mode           string    `json:"mode"`
	Revision       int64     `json:"revision"`
	Generation     int64     `json:"generation"`
	Active         bool      `json:"active"`
	CreatedAt      time.Time `json:"createdAt"`
	ExpiresAt      time.Time `json:"expiresAt"`
	LastUsedAt     time.Time `json:"lastUsedAt"`
	Digest         string    `json:"digest"`
	CSRFDigest     string    `json:"csrfDigest,omitempty"`
	PreviousDigest string    `json:"previousDigest,omitempty"`
	PreviousUntil  time.Time `json:"previousUntil"`
	Policy         Policy    `json:"policy"`
}
type Access struct{ data AccessData }

func NewAccess(d AccessData) Access       { d.Policy = d.Policy.Clone(); return Access{data: d} }
func (a Access) Snapshot() AccessData     { d := a.data; d.Policy = d.Policy.Clone(); return d }
func (a Access) Valid(now time.Time) bool { return a.data.Active && now.Before(a.data.ExpiresAt) }
func (a Access) Match(secret string, now time.Time) (bool, bool) {
	if !a.Valid(now) {
		return false, false
	}
	if Match(secret, a.data.Digest) {
		return true, false
	}
	if now.Before(a.data.PreviousUntil) && Match(secret, a.data.PreviousDigest) {
		return true, true
	}
	return false, false
}
func (a Access) View() AccessView {
	d := a.data
	return AccessView{ID: d.ID, ConnectionID: d.ConnectionID, ParentID: d.ParentID, Renewable: d.Renewable, Name: d.Name, Mode: d.Mode, Revision: d.Revision, Generation: d.Generation, Active: d.Active, CreatedAt: d.CreatedAt, ExpiresAt: d.ExpiresAt, LastUsedAt: d.LastUsedAt, Policy: d.Policy.Clone()}
}

type AccessView struct {
	ID           string    `json:"id"`
	ConnectionID string    `json:"connectionID"`
	ParentID     string    `json:"parentID,omitempty"`
	Renewable    bool      `json:"renewable"`
	Name         string    `json:"name"`
	Mode         string    `json:"mode"`
	Revision     int64     `json:"revision"`
	Generation   int64     `json:"generation"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"createdAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
	LastUsedAt   time.Time `json:"lastUsedAt"`
	Policy       Policy    `json:"policy"`
}

type Event struct {
	ID       string    `json:"id"`
	Owner    string    `json:"owner"`
	ActorID  string    `json:"actorID"`
	TargetID string    `json:"targetID"`
	Action   string    `json:"action"`
	At       time.Time `json:"at"`
}
type Secrets struct {
	AccessKey    string `json:"accessKey"`
	SecretKey    string `json:"secretKey"`
	SessionToken string `json:"sessionToken,omitempty"`
}

func (p Policy) Full() bool { return len(p.Resources) == 0 && p.Contains(FullPolicy()) }

func equalDigest(a, b string) bool {
	return len(a) == 64 && len(b) == 64 && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

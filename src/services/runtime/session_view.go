package runtime

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/url"
	"time"
)

// Session identity and connection configuration never change after construction.
func (s *Session) SessionID() string          { return s.id }
func (s *Session) ActorName() string          { return s.actor }
func (s *Session) CSRFToken() string          { return s.csrf }
func (s *Session) ExpiresAt() time.Time       { return s.expires }
func (s *Session) InternalHost() string       { return s.internal.Host }
func (s *Session) Expired(now time.Time) bool { return !s.expires.IsZero() && now.After(s.expires) }
func (s *Session) UsesBearer() bool           { return s.bearer }

func (s *Session) MatchesBearer(value string) bool {
	digest := sha256.Sum256([]byte(value))
	return s.bearer && value != "" && subtle.ConstantTimeCompare(digest[:], s.bearerDigest[:]) == 1
}

func (s *Session) MatchesToken(value string) bool {
	return !s.bearer && value != "" && subtle.ConstantTimeCompare([]byte(value), []byte(s.token)) == 1
}
func (s *Session) MatchesCSRF(value string) bool {
	return !s.bearer && value != "" && subtle.ConstantTimeCompare([]byte(value), []byte(s.csrf)) == 1
}

// CookieSnapshot is a detached transport value, never a JSON response DTO.
type CookieSnapshot struct {
	ID, Token string    `json:"-"`
	Expires   time.Time `json:"-"`
	Secure    bool      `json:"-"`
}

func (s *Session) CookieSnapshot() CookieSnapshot {
	return CookieSnapshot{ID: s.id, Token: s.token, Expires: s.expires, Secure: s.secureCookies}
}

func copyEndpoint(value *url.URL) *url.URL {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

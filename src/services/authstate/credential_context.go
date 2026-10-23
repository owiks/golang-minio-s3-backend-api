package authstate

import (
	"context"
	"time"
)

type credentialKey struct{}
type credentialStamp struct {
	id, digest string
	expires    time.Time
}

// WithCredential carries the already authenticated version to stored operation
// clients. It contains only a hash, never the presented secret or HTTP headers.
func WithCredential(ctx context.Context, id, digest string, expires time.Time) context.Context {
	return context.WithValue(ctx, credentialKey{}, credentialStamp{id: id, digest: digest, expires: expires})
}
func CredentialFor(ctx context.Context, id string) (string, time.Time, bool) {
	v, ok := ctx.Value(credentialKey{}).(credentialStamp)
	if ok && v.id != id {
		return "", time.Time{}, true
	}
	return v.digest, v.expires, ok
}

// DigestExpiry is the lifetime of this particular credential version, including
// a previous secret's overlap, rather than just the grant's absolute lifetime.
func (a Access) DigestExpiry(digest string, now time.Time) (time.Time, bool) {
	if !a.Valid(now) || len(digest) != 64 {
		return time.Time{}, false
	}
	if equalDigest(digest, a.data.Digest) {
		return a.data.ExpiresAt, true
	}
	if now.Before(a.data.PreviousUntil) && equalDigest(digest, a.data.PreviousDigest) {
		return EarlierExpiry(a.data.ExpiresAt, a.data.PreviousUntil), true
	}
	return time.Time{}, false
}

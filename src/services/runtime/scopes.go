package runtime

import (
	"context"
	"net/http"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

func (s *Session) livePolicy(ctx context.Context) (authstate.Policy, error) {
	if s.manager == nil {
		return s.AccessPolicy(), nil
	}
	a, c, err := s.manager.Lookup(ctx, s.id)
	if err != nil {
		return authstate.Policy{}, err
	}
	if _, valid := s.credentialExpiry(ctx, a, time.Now()); !valid || !c.Snapshot().Active || !s.currentSecrets(c) {
		return authstate.Policy{}, authstate.Denied()
	}
	return a.Snapshot().Policy, nil
}
func (s *Session) requestTransport() http.RoundTripper {
	if s.manager == nil && s.access.Snapshot().ID == "" {
		return s.transport
	}
	return storage.ScopeTransport{Base: s.transport, Policy: s.AccessPolicy(), Load: s.livePolicy, CheckCleanup: s.checkCleanupSecrets}
}
func (s *Session) adminTransport() http.RoundTripper {
	if s.manager == nil && s.access.Snapshot().ID == "" {
		return s.transport
	}
	return storage.ScopeTransport{Base: s.transport, Policy: s.AccessPolicy(), Load: s.livePolicy, Admin: true}
}
func (s *Session) signPermission(ctx context.Context, scope, bucket, key string, ttl time.Duration) (time.Duration, error) {
	p, expiry := s.AccessPolicy(), s.expires
	if s.manager != nil {
		a, c, err := s.manager.Lookup(ctx, s.id)
		if err != nil {
			return 0, err
		}
		if !c.Snapshot().Active || !s.currentSecrets(c) {
			return 0, authstate.Denied()
		}
		var valid bool
		expiry, valid = s.credentialExpiry(ctx, a, time.Now())
		if !valid {
			return 0, authstate.Denied()
		}
		p = a.Snapshot().Policy
	}
	if !p.Allows(scope) || !p.AllowsObject(bucket, key) {
		return 0, authstate.Denied()
	}
	if expiry.IsZero() {
		return ttl, nil
	}
	remaining := time.Until(expiry).Truncate(time.Second)
	if ttl > remaining {
		ttl = remaining
	}
	if ttl < time.Second {
		return 0, authstate.Denied()
	}
	return ttl, nil
}

// An operation retains the lifetime of the credential which started it. A later
// renewal or a fresh client must not extend a captured request's authorization.
func (s *Session) credentialExpiry(ctx context.Context, current authstate.Access, now time.Time) (time.Time, bool) {
	snapshot := s.access.Snapshot()
	digest, expiry := snapshot.Digest, s.expires
	if s.previous {
		digest = snapshot.PreviousDigest
	}
	if presented, deadline, ok := authstate.CredentialFor(ctx, s.id); ok {
		digest = presented
		expiry = authstate.EarlierExpiry(expiry, deadline)
	}
	deadline, valid := current.DigestExpiry(digest, now)
	if !valid {
		return time.Time{}, false
	}
	expiry = authstate.EarlierExpiry(expiry, deadline)
	return expiry, authstate.ExpiryValid(expiry, now)
}

// Bound clients keep one immutable native credential version. A replacement
// affects future request bindings, never a signature already being constructed.
func (s *Session) currentSecrets(c authstate.Connection) bool {
	d := c.Snapshot()
	return s.connectionID == d.ID && s.secretVersion == d.SecretVersion
}

func (s *Session) checkCleanupSecrets(ctx context.Context) error {
	if s.manager == nil {
		return nil
	}
	_, c, err := s.manager.Lookup(ctx, s.id)
	if err != nil {
		return err
	}
	if !s.currentSecrets(c) {
		return authstate.Denied()
	}
	return nil
}

// DelegationTTL authorizes the exact delegated prefix and limits its lifetime.
func (s *Session) DelegationTTL(ctx context.Context, scope, bucket, prefix string, seconds int) (int, error) {
	ttl, err := s.signPermission(ctx, scope, bucket, prefix, time.Duration(seconds)*time.Second)
	if err != nil {
		return 0, err
	}
	return int(ttl / time.Second), nil
}

// OwnerRetained fences runtime lifetime, not authentication. An unavailable
// repository is not evidence of revocation: retain bounded work until its own
// expiry; middleware and all native I/O still fail closed on that same error.
func (app *State) OwnerRetained(id string) bool {
	if cached := app.Session(id); cached != nil && (cached.sessionCore == nil || cached.connection.SecretKey == "") {
		return cached.expires.IsZero() || !cached.Expired(time.Now())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a, c, err := app.Auth.Lookup(ctx, id)
	if err != nil {
		return !authstate.IsMissing(err)
	}
	return a.Valid(time.Now()) && c.Snapshot().Active
}
func (app *State) CheckDelegation(ctx context.Context, id, scope, bucket, prefix string) error {
	a, c, err := app.Auth.Lookup(ctx, id)
	if err != nil {
		return err
	}
	p := a.Snapshot().Policy
	if !a.Valid(time.Now()) || !c.Snapshot().Active || !p.Allows(scope) || !p.AllowsObject(bucket, prefix) {
		return authstate.Denied()
	}
	return nil
}

func (s *Session) AuthorizeResource(ctx context.Context, scope, bucket, key string) error {
	p, err := s.livePolicy(ctx)
	if err != nil {
		return err
	}
	if !p.Allows(scope) || !p.AllowsObject(bucket, key) {
		return authstate.Denied()
	}
	return nil
}

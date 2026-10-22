package runtime

import (
	"context"
	"encoding/hex"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/endpoint"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

func (s *Session) ConnectionID() string     { return s.connectionID }
func (s *Session) Access() authstate.Access { return s.access }
func (s *Session) AccessGuard() authstate.Guard {
	return authstate.CredentialGuard(s.access, s.previous)
}
func (s *Session) AccessPolicy() authstate.Policy {
	if s.access.Snapshot().ID == "" {
		return authstate.FullPolicy()
	}
	return s.access.Snapshot().Policy
}
func (s *Session) AccessManager() *authstate.Manager { return s.manager }
func (app *State) LookupAccess(ctx context.Context, id string) (authstate.Access, authstate.Connection, error) {
	return app.Auth.Lookup(ctx, id)
}
func (app *State) BindAccess(ctx context.Context, a authstate.Access, _ authstate.Connection, previous bool) (*Session, error) {
	// Authentication may have raced with an upstream secret replacement. Read the
	// current connection rather than publishing clients from a stale ciphertext.
	c, err := app.bindingConnection(ctx, a, previous)
	if err != nil {
		return nil, err
	}
	d, cd := a.Snapshot(), c.Snapshot()
	root := app.Session(d.ID)
	if !matchesConnection(root, cd) {
		release, err := app.mutations.Acquire(ctx, "restore:"+d.ID)
		if err != nil {
			return nil, err
		}
		defer release()
		c, err = app.bindingConnection(ctx, a, previous)
		if err != nil {
			return nil, err
		}
		cd = c.Snapshot()
		root = app.Session(d.ID)
		if !matchesConnection(root, cd) {
			credentials, err := app.Auth.Decrypt(c)
			if err != nil {
				return nil, err
			}
			public, err := endpoint.Parse(cd.Endpoint)
			if err != nil {
				return nil, authstate.Unavailable()
			}
			tr, internal, err := endpoint.Transport(ctx, app.config.Endpoints, public)
			if err != nil {
				return nil, err
			}
			cfg := app.config
			cfg.SessionTTL = time.Until(d.ExpiresAt)
			root, err = NewConnectedSession(Connection{Endpoint: cd.Endpoint, Region: cd.Region, AccessKey: credentials.AccessKey, SecretKey: credentials.SecretKey, SessionToken: credentials.SessionToken}, cfg, public, internal, tr)
			if err != nil {
				tr.CloseIdleConnections()
				return nil, err
			}
			root.id = d.ID
			root.expires = d.ExpiresAt
			root.bearer = d.Mode == "bearer"
			root.connectionID = cd.ID
			root.connectionRevision = cd.Revision
			root.secretVersion = cd.SecretVersion
			root.token = ""
			root.csrf = ""
			if err = app.Sessions.Put(root); err != nil {
				tr.CloseIdleConnections()
				return nil, err
			}
		}
	}
	// Only immutable identity and auth snapshots are per request. Native resources
	// and their coordination owners remain shared, with no copying of locks.
	expiry := d.ExpiresAt
	if previous && d.PreviousUntil.Before(expiry) {
		expiry = d.PreviousUntil
	}
	bound := &Session{sessionCore: root.sessionCore, id: d.ID, expires: expiry, bearer: d.Mode == "bearer", access: a, previous: previous, manager: app.Auth}
	s3, err := storage.NewS3(bound.clientConfig(), bound.requestTransport())
	if err != nil {
		return nil, err
	}
	admin, err := storage.NewAdmin(bound.clientConfig(), bound.adminTransport())
	if err != nil {
		return nil, err
	}
	bound.boundS3 = s3
	bound.boundAdmin = admin
	return bound, nil
}

func matchesConnection(s *Session, c authstate.ConnectionData) bool {
	return s != nil && s.sessionCore != nil && s.connectionID == c.ID && s.connectionRevision == c.Revision && s.secretVersion == c.SecretVersion
}

func (app *State) bindingConnection(ctx context.Context, authenticated authstate.Access, previous bool) (authstate.Connection, error) {
	d := authenticated.Snapshot()
	current, connection, err := app.Auth.Lookup(ctx, d.ID)
	if err != nil {
		return authstate.Connection{}, err
	}
	digest := d.Digest
	if previous {
		digest = d.PreviousDigest
	}
	if _, valid := current.DigestExpiry(digest, time.Now()); !valid || !connection.Snapshot().Active || current.Snapshot().ConnectionID != d.ConnectionID {
		return authstate.Connection{}, authstate.Denied()
	}
	return connection, nil
}
func (app *State) StoreSessionContext(ctx context.Context, s *Session) error {
	if err := app.Sessions.Put(s); err != nil {
		return err
	}
	if s.sessionCore == nil || s.connection.SecretKey == "" {
		return nil
	}
	// The returned login object owns the one-time cookie/CSRF values; the SDK
	// cache retains neither plaintext credential.
	cached := &Session{sessionCore: s.sessionCore, id: s.id, bearer: s.bearer, expires: s.expires}
	if err := app.Sessions.Put(cached); err != nil {
		return err
	}
	mode := "cookie"
	digest := authstate.Digest(s.token)
	if s.bearer {
		mode = "bearer"
		digest = hex.EncodeToString(s.bearerDigest[:])
	}
	err := app.Auth.Bootstrap(ctx, authstate.Bootstrap{ID: s.id, ConnectionID: s.connectionID, Endpoint: s.public.String(), Region: s.region, Mode: mode, Digest: digest, CSRFDigest: authstate.Digest(s.csrf), ExpiresAt: s.expires, Secrets: authstate.Secrets{AccessKey: s.connection.AccessKey, SecretKey: s.connection.SecretKey, SessionToken: s.connection.SessionToken}})
	if err != nil {
		app.Sessions.Delete(s.id)
	}
	return err
}
func (app *State) CloseAccess(ctx context.Context, s *Session) error {
	if err := app.Auth.Logout(ctx, s.AccessGuard()); err != nil {
		return err
	}
	app.removeCachedSession(s.SessionID())
	return nil
}

func (s *Session) RequestContext(ctx context.Context) context.Context {
	d := s.access.Snapshot()
	digest := d.Digest
	if s.previous {
		digest = d.PreviousDigest
	}
	return authstate.WithCredential(ctx, s.id, digest, s.expires)
}

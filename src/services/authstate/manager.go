package authstate

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

type Manager struct {
	repository Repository
	box        SecretBox
	maxTTL     time.Duration
	verifier   CredentialVerifier
}

func NewManager(repository Repository, box SecretBox, maxTTL time.Duration) *Manager {
	return &Manager{repository: repository, box: box, maxTTL: maxTTL}
}
func (m *Manager) Close() error { return m.repository.Close() }
func (m *Manager) Lookup(ctx context.Context, id string) (Access, Connection, error) {
	return m.repository.Get(ctx, id)
}
func (m *Manager) Touch(ctx context.Context, id string, now time.Time) error {
	return m.repository.Touch(ctx, id, now)
}
func (m *Manager) Check(ctx context.Context) error { return m.repository.Ping(ctx) }

type Bootstrap struct {
	ID, ConnectionID, Endpoint, Region, Mode, Digest, CSRFDigest string
	ExpiresAt                                                    time.Time
	Secrets                                                      Secrets
}

func (m *Manager) Bootstrap(ctx context.Context, input Bootstrap) error {
	if !validSecrets(input.Secrets) {
		return Invalid()
	}
	now := time.Now().UTC()
	owner := Owner(input.Endpoint, input.Secrets.AccessKey)
	c := ConnectionData{ID: input.ConnectionID, Owner: owner, Endpoint: input.Endpoint, Region: input.Region, Actor: Digest(input.Secrets.AccessKey), Revision: 1, SecretVersion: 1, Active: true, CreatedAt: now}
	raw, err := json.Marshal(input.Secrets)
	if err != nil {
		return Invalid()
	}
	defer clear(raw)
	c.Cipher, err = m.box.Seal(raw, NewConnection(c).AAD())
	if err != nil {
		return Unavailable()
	}
	a := NewAccess(AccessData{ID: input.ID, ConnectionID: c.ID, Owner: owner, Mode: input.Mode, Renewable: input.Mode == "bearer", Revision: 1, Generation: 1, Active: true, CreatedAt: now, ExpiresAt: input.ExpiresAt, Digest: input.Digest, CSRFDigest: input.CSRFDigest, Policy: FullPolicy()})
	return m.repository.Bootstrap(ctx, NewConnection(c), a, event(owner, input.ID, input.ID, "access.created"))
}
func (m *Manager) Decrypt(c Connection) (Secrets, error) {
	d := c.Snapshot()
	if !d.Active {
		return Secrets{}, NotFound()
	}
	raw, err := m.box.Open(d.Cipher, c.AAD())
	if err != nil {
		return Secrets{}, Unavailable()
	}
	defer clear(raw)
	var secrets Secrets
	if json.Unmarshal(raw, &secrets) != nil || secrets.AccessKey == "" || secrets.SecretKey == "" || Owner(d.Endpoint, secrets.AccessKey) != d.Owner {
		return Secrets{}, Unavailable()
	}
	return secrets, nil
}
func CredentialGuard(a Access, previous bool) Guard {
	d := a.Snapshot()
	return Guard{Owner: d.Owner, ActorID: d.ID, Revision: d.Revision, Generation: d.Generation, Previous: previous}
}
func (m *Manager) manage(ctx context.Context, g Guard) (Access, error) {
	return m.guardedAccess(ctx, g, true)
}

func (m *Manager) guardedAccess(ctx context.Context, g Guard, management bool) (Access, error) {
	a, c, err := m.repository.Get(ctx, g.ActorID)
	if err != nil {
		return Access{}, err
	}
	d := a.Snapshot()
	if !a.Valid(time.Now()) || !c.Snapshot().Active || d.Owner != g.Owner || d.Revision != g.Revision || d.Generation != g.Generation || g.Previous || management && !d.Policy.Allows("access:manage") {
		return Access{}, Denied()
	}
	return a, nil
}
func (m *Manager) Connections(ctx context.Context, g Guard) ([]ConnectionView, error) {
	actor, err := m.manage(ctx, g)
	if err != nil {
		return nil, err
	}
	rows, err := m.repository.Connections(ctx, g.Owner)
	if err != nil {
		return nil, err
	}
	for _, c := range rows {
		if c.Snapshot().ID != actor.Snapshot().ConnectionID {
			if err = m.cross(ctx, g, c.Snapshot().ID); err != nil {
				return nil, err
			}
			break
		}
	}
	out := []ConnectionView{}
	for _, c := range rows {
		out = append(out, c.View())
	}
	return out, nil
}
func (m *Manager) Connection(ctx context.Context, g Guard, id string) (Connection, error) {
	if _, err := m.manage(ctx, g); err != nil {
		return Connection{}, err
	}
	c, err := m.ownedConnection(ctx, g.Owner, id)
	if err != nil {
		return Connection{}, err
	}
	if err = m.cross(ctx, g, id); err != nil {
		return Connection{}, err
	}
	return c, nil
}
func (m *Manager) ownedConnection(ctx context.Context, owner, id string) (Connection, error) {
	rows, err := m.repository.Connections(ctx, owner)
	if err != nil {
		return Connection{}, err
	}
	for _, c := range rows {
		if c.Snapshot().ID == id {
			return c, nil
		}
	}
	return Connection{}, NotFound()
}
func (m *Manager) Accesses(ctx context.Context, g Guard, cid string) ([]AccessView, error) {
	actor, err := m.manage(ctx, g)
	if err != nil {
		return nil, err
	}
	rows, err := m.repository.Accesses(ctx, g.Owner, cid)
	if err != nil {
		return nil, err
	}
	if err = m.cross(ctx, g, cid); err != nil {
		return nil, err
	}
	out := []AccessView{}
	for _, a := range rows {
		if a.Snapshot().Mode == "bearer" && actor.Snapshot().Policy.Contains(a.Snapshot().Policy) {
			out = append(out, a.View())
		}
	}
	return out, nil
}
func (m *Manager) Events(ctx context.Context, g Guard) ([]Event, error) {
	a, err := m.manage(ctx, g)
	if err != nil {
		return nil, err
	}
	if !a.Snapshot().Policy.Full() {
		return nil, Denied()
	}
	rows, err := m.repository.Events(ctx, g.Owner)
	if err != nil {
		return nil, err
	}
	if err = m.cross(ctx, g, ""); err != nil {
		return nil, err
	}
	return rows, nil
}

type IssueRequest struct {
	Name       string `json:"name"`
	TTLSeconds int64  `json:"ttlSeconds"`
	Policy     Policy `json:"policy"`
}
type Issued struct {
	ID        string    `json:"id"`
	Token     string    `json:"token"`
	TokenType string    `json:"tokenType"`
	ExpiresAt time.Time `json:"expiresAt"`
	ExpiresIn int64     `json:"expiresIn"`
	Revision  int64     `json:"revision"`
}

func (m *Manager) Issue(ctx context.Context, g Guard, cid string, r IssueRequest) (Issued, error) {
	actor, err := m.manage(ctx, g)
	if err != nil {
		return Issued{}, err
	}
	if len(r.Name) > 128 || !r.Policy.Valid() || r.TTLSeconds < 1 || r.TTLSeconds > int64(m.maxTTL/time.Second) {
		return Issued{}, Invalid()
	}
	connection, err := m.Connection(ctx, g, cid)
	if err != nil {
		return Issued{}, err
	}
	now := time.Now().UTC()
	expiry := now.Add(time.Duration(r.TTLSeconds) * time.Second)
	parent := ""
	if actor.Snapshot().Mode == "bearer" {
		parent = actor.Snapshot().ID
		expiry = minTime(expiry, actor.Snapshot().ExpiresAt)
	}
	token := securetoken.New()
	id := securetoken.New()
	access := NewAccess(AccessData{ID: id, Owner: g.Owner, ConnectionID: cid, ParentID: parent, Renewable: true, Name: r.Name, Mode: "bearer", Revision: 1, Generation: 1, Active: true, CreatedAt: now, ExpiresAt: expiry, Digest: Digest(token), Policy: r.Policy})
	result, err := m.repository.Apply(ctx, Command{Kind: "create", Guard: g, ConnectionRevision: connection.Snapshot().Revision, Access: access, Event: event(g.Owner, g.ActorID, id, "access.created")})
	if err != nil {
		return Issued{}, err
	}
	return issued(result.Access, token), nil
}
func issued(a Access, token string) Issued {
	d := a.Snapshot()
	return Issued{ID: d.ID, Token: token, TokenType: "Bearer", ExpiresAt: d.ExpiresAt, ExpiresIn: max(0, int64(time.Until(d.ExpiresAt).Seconds())), Revision: d.Revision}
}

type RotateRequest struct {
	Revision     int64 `json:"revision" validate:"required"`
	GraceSeconds int64 `json:"graceSeconds"`
}

func (m *Manager) Rotate(ctx context.Context, g Guard, id string, r RotateRequest) (Issued, error) {
	if r.Revision < 1 || r.GraceSeconds < 0 || r.GraceSeconds > 60 {
		return Issued{}, Invalid()
	}
	if err := m.authorizeToken(ctx, g, id); err != nil {
		return Issued{}, err
	}
	token := securetoken.New()
	result, err := m.repository.Apply(ctx, Command{Kind: "rotate", Guard: g, TargetID: id, Revision: r.Revision, Digest: Digest(token), GraceSeconds: r.GraceSeconds, Event: event(g.Owner, g.ActorID, id, "access.rotated")})
	if err != nil {
		return Issued{}, err
	}
	return issued(result.Access, token), nil
}

type RenewRequest struct {
	Revision   int64 `json:"revision" validate:"required"`
	TTLSeconds int64 `json:"ttlSeconds" validate:"required"`
}

func (m *Manager) Renew(ctx context.Context, g Guard, id string, r RenewRequest) (AccessView, error) {
	if r.Revision < 1 || r.TTLSeconds < 1 || r.TTLSeconds > int64(m.maxTTL/time.Second) {
		return AccessView{}, Invalid()
	}
	actor, err := m.guardedAccess(ctx, g, id != g.ActorID)
	if err != nil {
		return AccessView{}, err
	}
	if id != g.ActorID {
		if err = m.authorizeToken(ctx, g, id); err != nil {
			return AccessView{}, err
		}
	}
	expiry := time.Now().Add(time.Duration(r.TTLSeconds) * time.Second)
	if id != g.ActorID || !actor.Snapshot().Renewable {
		expiry = minTime(expiry, actor.Snapshot().ExpiresAt)
	}
	result, err := m.repository.Apply(ctx, Command{Kind: "renew", Guard: g, TargetID: id, Revision: r.Revision, ExpiresAt: expiry.UnixMicro(), Event: event(g.Owner, g.ActorID, id, "access.renewed")})
	if err != nil {
		return AccessView{}, err
	}
	return result.Access.View(), nil
}

type EditRequest struct {
	Revision int64   `json:"revision" validate:"required"`
	Name     *string `json:"name,omitempty"`
	Policy   *Policy `json:"policy,omitempty"`
}

func (m *Manager) Edit(ctx context.Context, g Guard, id string, r EditRequest) (AccessView, error) {
	if r.Revision < 1 || r.Name == nil && r.Policy == nil {
		return AccessView{}, Invalid()
	}
	if err := m.authorizeToken(ctx, g, id); err != nil {
		return AccessView{}, err
	}
	result, err := m.repository.Apply(ctx, Command{Kind: "edit", Guard: g, TargetID: id, Revision: r.Revision, Name: r.Name, Policy: r.Policy, Event: event(g.Owner, g.ActorID, id, "access.updated")})
	if err != nil {
		return AccessView{}, err
	}
	return result.Access.View(), nil
}
func (m *Manager) Revoke(ctx context.Context, g Guard, id string, revision int64) error {
	if revision < 1 {
		return Invalid()
	}
	if err := m.authorizeToken(ctx, g, id); err != nil {
		return err
	}
	_, err := m.repository.Apply(ctx, Command{Kind: "revoke", Guard: g, TargetID: id, Revision: revision, Event: event(g.Owner, g.ActorID, id, "access.revoked")})
	return err
}
func (m *Manager) Logout(ctx context.Context, g Guard) error {
	_, err := m.repository.Apply(ctx, Command{Kind: "logout", Guard: g, TargetID: g.ActorID, Revision: g.Revision, Event: event(g.Owner, g.ActorID, g.ActorID, "access.closed")})
	return err
}
func (m *Manager) DeleteConnection(ctx context.Context, g Guard, id string, revision int64) error {
	if revision < 1 {
		return Invalid()
	}
	if _, err := m.Connection(ctx, g, id); err != nil {
		return err
	}
	_, err := m.repository.Apply(ctx, Command{Kind: "revoke_connection", Guard: g, TargetID: id, Revision: revision, Event: event(g.Owner, g.ActorID, id, "connection.revoked")})
	return err
}
func (m *Manager) ReplaceCredentials(ctx context.Context, g Guard, id string, revision int64, secrets Secrets, verifier CredentialVerifier) error {
	secrets.AccessKey = strings.TrimSpace(secrets.AccessKey)
	secrets.SessionToken = strings.TrimSpace(secrets.SessionToken)
	if !validSecrets(secrets) {
		return Invalid()
	}
	actor, err := m.manage(ctx, g)
	if err != nil {
		return err
	}
	if !actor.Snapshot().Policy.Full() || verifier == nil {
		return Denied()
	}
	c, err := m.ownedConnection(ctx, g.Owner, id)
	if err != nil {
		return err
	}
	d := c.Snapshot()
	if revision < 1 || d.Revision != revision {
		return Conflict()
	}
	if secrets.AccessKey == "" || secrets.SecretKey == "" || Owner(d.Endpoint, secrets.AccessKey) != d.Owner {
		return Denied()
	}
	if err = verifier.Verify(ctx, c.View(), secrets); err != nil {
		return err
	}
	d.SecretVersion++
	raw, err := json.Marshal(secrets)
	if err != nil {
		return Invalid()
	}
	defer clear(raw)
	cipher, err := m.box.Seal(raw, NewConnection(d).AAD())
	if err != nil {
		return Unavailable()
	}
	_, err = m.repository.Apply(ctx, Command{Kind: "credentials", Guard: g, TargetID: id, Revision: revision, Cipher: cipher, SecretVersion: d.SecretVersion, Event: event(g.Owner, g.ActorID, id, "connection.credentials_changed")})
	return err
}
func event(owner, actor, target, action string) Event {
	return Event{ID: securetoken.New(), Owner: owner, ActorID: actor, TargetID: target, Action: action, At: time.Now().UTC()}
}
func IsMissing(err error) bool { return apperror.Public(err).Code == "AccessStateNotFound" }

func (m *Manager) Token(ctx context.Context, g Guard, id string) (AccessView, error) {
	actor, err := m.manage(ctx, g)
	if err != nil {
		return AccessView{}, err
	}
	a, _, err := m.repository.Get(ctx, id)
	if err != nil {
		return AccessView{}, err
	}
	d := a.Snapshot()
	if d.Owner != g.Owner || d.Mode != "bearer" || !actor.Snapshot().Policy.Contains(d.Policy) {
		return AccessView{}, NotFound()
	}
	if err = m.cross(ctx, g, d.ConnectionID); err != nil {
		return AccessView{}, err
	}
	return a.View(), nil
}

// WithVerifier returns request-local wiring over the same immutable dependencies.
// It never replaces the live application's repository or keyring.
func (m *Manager) WithVerifier(v CredentialVerifier) *Manager {
	copy := *m
	copy.verifier = v
	return &copy
}
func (m *Manager) cross(ctx context.Context, g Guard, targetConnection string) error {
	actor, err := m.manage(ctx, g)
	if err != nil {
		return err
	}
	if targetConnection == actor.Snapshot().ConnectionID {
		return nil
	}
	if m.verifier == nil {
		return Denied()
	}
	_, c, err := m.repository.Get(ctx, g.ActorID)
	if err != nil {
		return err
	}
	secrets, err := m.Decrypt(c)
	if err != nil {
		return err
	}
	return m.verifier.Verify(ctx, c.View(), secrets)
}
func (m *Manager) authorizeToken(ctx context.Context, g Guard, id string) error {
	actor, err := m.manage(ctx, g)
	if err != nil {
		return err
	}
	a, _, err := m.repository.Get(ctx, id)
	if err != nil {
		return err
	}
	d := a.Snapshot()
	if d.Owner != g.Owner || d.Mode != "bearer" || !actor.Snapshot().Policy.Contains(d.Policy) {
		return NotFound()
	}
	return m.cross(ctx, g, d.ConnectionID)
}

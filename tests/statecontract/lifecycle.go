package statecontract

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/secretbox"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

// Each case has an independent owner so that the same assertions exercise both
// in-memory and SQL adapters without relying on record ordering or test sleeps.
func runLifecycle(t *testing.T, repo authstate.Repository) {
	t.Helper()
	t.Run("renewable-service-root", func(t *testing.T) {
		f := newLifecycle(t, repo)
		root, secret := f.bootstrap("bearer", 5*time.Minute)
		before := root.Snapshot()
		readOnly := authstate.Policy{Scopes: []string{"objects:read"}, Resources: []authstate.Resource{{Bucket: "data", Prefix: "reports/"}}}
		updated, err := f.manager.Edit(f.ctx, guard(root), before.ID, authstate.EditRequest{Revision: before.Revision, Policy: &readOnly})
		if err != nil {
			t.Fatal(err)
		}
		root = f.lookup(before.ID)
		if !root.Snapshot().Renewable || root.Snapshot().ParentID != "" {
			t.Fatal("fresh bearer is not a renewable service root")
		}
		renewed, err := f.manager.Renew(f.ctx, guard(root), before.ID, authstate.RenewRequest{Revision: updated.Revision, TTLSeconds: 900})
		if err != nil || !renewed.ExpiresAt.After(before.ExpiresAt) || !reflect.DeepEqual(renewed.Policy, readOnly) {
			t.Fatal("self-renew failed or changed permissions", renewed, err)
		}
		after := f.lookup(before.ID)
		if after.Snapshot().Digest != before.Digest || after.Snapshot().Generation != before.Generation {
			t.Fatal("renewal replaced the credential")
		}
		if ok, previous := after.Match(secret, time.Now()); !ok || previous {
			t.Fatal("renewal invalidated the current secret")
		}
		if _, err = f.manager.Renew(f.ctx, guard(after), before.ID, authstate.RenewRequest{Revision: updated.Revision, TTLSeconds: 900}); apperror.Public(err).Status != 409 {
			t.Fatal("stale target revision accepted", err)
		}
		if _, err = f.manager.Renew(f.ctx, guard(root), before.ID, authstate.RenewRequest{Revision: renewed.Revision, TTLSeconds: 900}); apperror.Public(err).Status != 403 {
			t.Fatal("stale actor guard accepted", err)
		}
		if _, err = f.manager.Renew(f.ctx, guard(after), before.ID, authstate.RenewRequest{Revision: renewed.Revision, TTLSeconds: 3601}); apperror.Public(err).Status != 400 {
			t.Fatal("maximum lifetime bypassed", err)
		}
		raw, _ := json.Marshal(renewed)
		if strings.Contains(string(raw), secret) || strings.Contains(string(raw), before.Digest) {
			t.Fatal("renewal exposed a secret or its hash")
		}
	})
	t.Run("cookie-logout-keeps-service-root", func(t *testing.T) {
		f := newLifecycle(t, repo)
		cookie, _ := f.bootstrap("cookie", 2*time.Minute)
		if cookie.Snapshot().Renewable {
			t.Fatal("web cookie unexpectedly self-renewable")
		}
		service := f.issue(cookie, cookie.Snapshot().ConnectionID, authstate.Policy{Scopes: []string{"objects:read"}}, 900)
		saved := f.lookup(service.ID)
		if !saved.Snapshot().Renewable || saved.Snapshot().ParentID != "" || !saved.Snapshot().ExpiresAt.After(cookie.Snapshot().ExpiresAt) {
			t.Fatal("service credential inherits the browser session lifetime")
		}
		if err := f.manager.Logout(f.ctx, guard(cookie)); err != nil {
			t.Fatal(err)
		}
		current := f.lookup(service.ID)
		if !reflect.DeepEqual(saved.Snapshot(), current.Snapshot()) || !current.Valid(time.Now()) {
			t.Fatal("browser logout changed an independent service credential")
		}
		if _, err := f.manager.Renew(f.ctx, guard(current), service.ID, authstate.RenewRequest{Revision: 1, TTLSeconds: 1800}); err != nil {
			t.Fatal("independent read-only service cannot renew", err)
		}
	})
	t.Run("legacy-and-expired-access", func(t *testing.T) {
		f := newLifecycle(t, repo)
		root, _ := f.bootstrap("bearer", 5*time.Minute)
		_, connection, err := f.manager.Lookup(f.ctx, root.Snapshot().ID)
		if err != nil {
			t.Fatal(err)
		}
		legacyConnection := connection.Snapshot()
		legacyConnection.ID = securetoken.New()
		raw, _ := json.Marshal(authstate.Secrets{AccessKey: f.key, SecretKey: "fixture-secret"})
		legacyConnection.Cipher, err = f.box.Seal(raw, authstate.NewConnection(legacyConnection).AAD())
		if err != nil {
			t.Fatal(err)
		}
		legacy := root.Snapshot()
		legacy.ID, legacy.ConnectionID, legacy.Renewable = securetoken.New(), legacyConnection.ID, false
		event := authstate.Event{ID: securetoken.New(), Owner: legacy.Owner, ActorID: legacy.ID, TargetID: legacy.ID, Action: "access.created", At: time.Now().UTC()}
		if err = repo.Bootstrap(f.ctx, authstate.NewConnection(legacyConnection), authstate.NewAccess(legacy), event); err != nil {
			t.Fatal(err)
		}
		renewed, err := f.manager.Renew(f.ctx, guard(authstate.NewAccess(legacy)), legacy.ID, authstate.RenewRequest{Revision: 1, TTLSeconds: 900})
		if err != nil || renewed.Renewable || renewed.ExpiresAt.After(legacy.ExpiresAt) {
			t.Fatal("legacy access gained indefinite renewal", renewed, err)
		}
		expired, _ := f.bootstrap("bearer", -time.Minute)
		if _, err = f.manager.Renew(f.ctx, guard(expired), expired.Snapshot().ID, authstate.RenewRequest{Revision: 1, TTLSeconds: 60}); apperror.Public(err).Status != 403 {
			t.Fatal("expired caller resurrected its credential", err)
		}
		if _, err = f.manager.Renew(f.ctx, guard(root), expired.Snapshot().ID, authstate.RenewRequest{Revision: 1, TTLSeconds: 60}); apperror.Public(err).Status != 409 {
			t.Fatal("manager resurrected expired credential", err)
		}
	})
	t.Run("delegation-policy-and-expiry", func(t *testing.T) {
		f := newLifecycle(t, repo)
		root, _ := f.bootstrap("bearer", time.Hour)
		other, _ := f.bootstrap("bearer", time.Hour)
		managerPolicy := authstate.Policy{Scopes: []string{"access:manage", "objects:read", "objects:write"}, Resources: []authstate.Resource{{Bucket: "data", Prefix: "reports/"}, {Bucket: "archive", Prefix: "safe/"}}}
		manager := f.issue(root, root.Snapshot().ConnectionID, managerPolicy, 1800)
		parent := f.lookup(manager.ID)
		delegatedPolicy := authstate.Policy{Scopes: []string{"access:manage", "objects:read", "objects:write"}, Resources: []authstate.Resource{{Bucket: "data", Prefix: "reports/2026/"}, {Bucket: "archive", Prefix: "safe/"}}}
		delegated := f.issue(parent, other.Snapshot().ConnectionID, delegatedPolicy, 1200)
		child := f.lookup(delegated.ID)
		leaf := f.issue(child, other.Snapshot().ConnectionID, authstate.Policy{Scopes: []string{"objects:read"}, Resources: []authstate.Resource{{Bucket: "archive", Prefix: "safe/"}}}, 900)
		if child.Snapshot().ParentID != manager.ID || f.lookup(leaf.ID).Snapshot().ParentID != delegated.ID {
			t.Fatal("delegation ancestry missing")
		}
		narrow := authstate.Policy{Scopes: []string{"access:manage", "objects:read"}, Resources: []authstate.Resource{{Bucket: "data", Prefix: "reports/2026/daily/"}}}
		history, err := f.manager.Events(f.ctx, guard(root))
		if err != nil || len(history) == 0 {
			t.Fatal("cannot arrange duplicate audit event", err)
		}
		duplicate := history[0]
		duplicate.ActorID, duplicate.TargetID, duplicate.Action = root.Snapshot().ID, manager.ID, "access.updated"
		leafBefore := f.lookup(leaf.ID).Snapshot()
		if _, err = repo.Apply(f.ctx, authstate.Command{Kind: "edit", Guard: guard(root), TargetID: manager.ID, Revision: 1, Policy: &narrow, Event: duplicate}); err == nil {
			t.Fatal("cascade accepted an existing audit event")
		}
		if !reflect.DeepEqual(f.lookup(manager.ID).Snapshot(), parent.Snapshot()) || !reflect.DeepEqual(f.lookup(delegated.ID).Snapshot(), child.Snapshot()) || !reflect.DeepEqual(f.lookup(leaf.ID).Snapshot(), leafBefore) {
			t.Fatal("failed audit partially published a cascading update")
		}
		if _, err := f.manager.Edit(f.ctx, guard(root), manager.ID, authstate.EditRequest{Revision: 1, Policy: &narrow}); err != nil {
			t.Fatal(err)
		}
		after := f.lookup(delegated.ID)
		if !after.Valid(time.Now()) || !narrow.Contains(after.Snapshot().Policy) || after.Snapshot().Policy.Allows("objects:write") || !after.Snapshot().Policy.AllowsObject("data", "reports/2026/daily/file") || after.Snapshot().Policy.AllowsObject("data", "reports/2026/file") || after.Snapshot().Policy.AllowsObject("archive", "safe/file") {
			t.Fatal("cross-connection child did not inherit narrowed resources", after.View())
		}
		if after.Snapshot().Revision <= child.Snapshot().Revision {
			t.Fatal("narrowing left the old child guard usable")
		}
		if _, err := f.manager.Renew(f.ctx, guard(child), delegated.ID, authstate.RenewRequest{Revision: after.Snapshot().Revision, TTLSeconds: 60}); apperror.Public(err).Status != 403 {
			t.Fatal("pre-cascade child guard accepted", err)
		}
		if f.lookup(leaf.ID).Valid(time.Now()) {
			t.Fatal("empty resource intersection became unrestricted")
		}
		parent = f.lookup(manager.ID)
		renewed, err := f.manager.Renew(f.ctx, guard(root), manager.ID, authstate.RenewRequest{Revision: parent.Snapshot().Revision, TTLSeconds: 60})
		if err != nil {
			t.Fatal(err)
		}
		after = f.lookup(delegated.ID)
		if after.Snapshot().ExpiresAt.After(renewed.ExpiresAt) {
			t.Fatal("shortened parent left a longer-lived descendant")
		}
		selfRenewed, err := f.manager.Renew(f.ctx, guard(after), delegated.ID, authstate.RenewRequest{Revision: after.Snapshot().Revision, TTLSeconds: 3600})
		if err != nil || selfRenewed.ExpiresAt.After(renewed.ExpiresAt) {
			t.Fatal("child self-renew exceeded its parent", selfRenewed, err)
		}
		parent = f.lookup(manager.ID)
		if _, err = f.manager.Rotate(f.ctx, guard(root), manager.ID, authstate.RotateRequest{Revision: parent.Snapshot().Revision}); err != nil {
			t.Fatal(err)
		}
		if !f.lookup(delegated.ID).Valid(time.Now()) {
			t.Fatal("secret rotation revoked a child")
		}
		parent = f.lookup(manager.ID)
		if err = f.manager.Revoke(f.ctx, guard(root), manager.ID, parent.Snapshot().Revision); err != nil {
			t.Fatal(err)
		}
		after = f.lookup(delegated.ID)
		if after.Valid(time.Now()) || f.lookup(leaf.ID).Valid(time.Now()) {
			t.Fatal("parent revocation did not cascade across connections")
		}
		if _, err = f.manager.Renew(f.ctx, guard(root), delegated.ID, authstate.RenewRequest{Revision: after.Snapshot().Revision, TTLSeconds: 60}); apperror.Public(err).Status != 409 {
			t.Fatal("revoked descendant resurrected", err)
		}
	})
	t.Run("credential-replacement-and-connection-revoke", func(t *testing.T) {
		f := newLifecycle(t, repo)
		root, _ := f.bootstrap("bearer", time.Hour)
		other, _ := f.bootstrap("bearer", time.Hour)
		manager := f.issue(root, root.Snapshot().ConnectionID, authstate.FullPolicy(), 1800)
		parent := f.lookup(manager.ID)
		child := f.issue(parent, other.Snapshot().ConnectionID, authstate.Policy{Scopes: []string{"objects:read"}}, 900)
		before := []authstate.AccessData{root.Snapshot(), parent.Snapshot(), f.lookup(child.ID).Snapshot()}
		if err := f.manager.ReplaceCredentials(f.ctx, guard(root), root.Snapshot().ConnectionID, 1, authstate.Secrets{AccessKey: f.key, SecretKey: "replacement-secret"}, nativeProof{}); err != nil {
			t.Fatal(err)
		}
		for _, saved := range before {
			if !reflect.DeepEqual(f.lookup(saved.ID).Snapshot(), saved) {
				t.Fatal("upstream credential replacement mutated an API grant", saved.ID)
			}
		}
		_, connection, err := f.manager.Lookup(f.ctx, root.Snapshot().ID)
		if err != nil || connection.Snapshot().Revision != 2 || connection.Snapshot().SecretVersion != 2 {
			t.Fatal("connection version not advanced", err)
		}
		if secrets, err := f.manager.Decrypt(connection); err != nil || secrets.SecretKey != "replacement-secret" {
			t.Fatal("replacement not decryptable", err)
		}
		if err = f.manager.ReplaceCredentials(f.ctx, guard(root), root.Snapshot().ConnectionID, 1, authstate.Secrets{AccessKey: f.key, SecretKey: "stale-secret"}, nativeProof{}); apperror.Public(err).Status != 409 {
			t.Fatal("stale replacement accepted", err)
		}
		if err = f.manager.DeleteConnection(f.ctx, guard(root), connection.Snapshot().ID, 2); err != nil {
			t.Fatal(err)
		}
		if f.lookup(child.ID).Valid(time.Now()) || f.lookup(manager.ID).Valid(time.Now()) || f.lookup(root.Snapshot().ID).Valid(time.Now()) || !f.lookup(other.Snapshot().ID).Valid(time.Now()) {
			t.Fatal("connection deletion did not follow delegation ancestry")
		}
	})
}

type lifecycle struct {
	t       *testing.T
	ctx     context.Context
	manager *authstate.Manager
	box     authstate.SecretBox
	key     string
}

func newLifecycle(t *testing.T, repo authstate.Repository) lifecycle {
	t.Helper()
	box, err := secretbox.Ephemeral()
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle{t: t, ctx: context.Background(), manager: authstate.NewManager(repo, box, time.Hour).WithVerifier(nativeProof{}), box: box, key: "fixture-" + securetoken.New()}
}

func (f lifecycle) bootstrap(mode string, ttl time.Duration) (authstate.Access, string) {
	f.t.Helper()
	secret := securetoken.New()
	b := authstate.Bootstrap{ID: securetoken.New(), ConnectionID: securetoken.New(), Endpoint: "https://s3.example.test", Region: "us-east-1", Mode: mode, Digest: authstate.Digest(secret), ExpiresAt: time.Now().Add(ttl), Secrets: authstate.Secrets{AccessKey: f.key, SecretKey: "fixture-secret"}}
	if mode == "cookie" {
		b.CSRFDigest = authstate.Digest(securetoken.New())
	}
	if err := f.manager.Bootstrap(f.ctx, b); err != nil {
		f.t.Fatal(err)
	}
	return f.lookup(b.ID), secret
}

func (f lifecycle) lookup(id string) authstate.Access {
	f.t.Helper()
	a, _, err := f.manager.Lookup(f.ctx, id)
	if err != nil {
		f.t.Fatal(err)
	}
	return a
}

func (f lifecycle) issue(actor authstate.Access, connection string, policy authstate.Policy, ttl int64) authstate.Issued {
	f.t.Helper()
	v, err := f.manager.Issue(f.ctx, guard(actor), connection, authstate.IssueRequest{Name: "lifecycle service", TTLSeconds: ttl, Policy: policy})
	if err != nil {
		f.t.Fatal(err)
	}
	return v
}

func guard(a authstate.Access) authstate.Guard { return authstate.CredentialGuard(a, false) }

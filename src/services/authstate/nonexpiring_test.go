package authstate_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/secretbox"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
)

type nonexpiringFixture struct {
	t       *testing.T
	manager *authstate.Manager
	repo    authstate.Repository
	now     time.Time
	actorID string
	cid     string
}

func newNonexpiringFixture(t *testing.T, mode string, finite bool) nonexpiringFixture {
	t.Helper()
	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	repo := authstate.NewMemoryWithClock(clock)
	t.Cleanup(func() { _ = repo.Close() })
	box, err := secretbox.Ephemeral()
	if err != nil {
		t.Fatal(err)
	}
	manager := authstate.NewManagerWithClock(repo, box, time.Hour, clock)
	expiry := time.Time{}
	if finite {
		expiry = now.Add(time.Hour)
	}
	if err = manager.Bootstrap(context.Background(), authstate.Bootstrap{
		ID: "fixture-actor", ConnectionID: "fixture-connection", Endpoint: "https://s3.example.test", Mode: mode,
		Digest: authstate.Digest("fixture-actor-secret"), CSRFDigest: authstate.Digest("fixture-csrf"), ExpiresAt: expiry,
		Secrets: authstate.Secrets{AccessKey: "fixture-key", SecretKey: "fixture-secret"},
	}); err != nil {
		t.Fatal(err)
	}
	return nonexpiringFixture{t: t, manager: manager, repo: repo, now: now, actorID: "fixture-actor", cid: "fixture-connection"}
}

func (f nonexpiringFixture) access(id string) authstate.Access {
	f.t.Helper()
	a, _, err := f.manager.Lookup(context.Background(), id)
	if err != nil {
		f.t.Fatal(err)
	}
	return a
}

func (f nonexpiringFixture) guard(id string) authstate.Guard {
	return authstate.CredentialGuard(f.access(id), false)
}

func (f nonexpiringFixture) issue(actor string, ttl int64) authstate.Issued {
	f.t.Helper()
	issued, err := f.manager.Issue(context.Background(), f.guard(actor), f.cid, authstate.IssueRequest{Name: "service", TTLSeconds: ttl, Policy: authstate.FullPolicy()})
	if err != nil {
		f.t.Fatal(err)
	}
	return issued
}

func TestNonexpiringIssueUsesBearerAncestryInsteadOfCookieLifetime(t *testing.T) {
	for _, mode := range []string{"cookie", "bearer"} {
		t.Run(mode, func(t *testing.T) {
			f := newNonexpiringFixture(t, mode, true)
			issued := f.issue(f.actorID, 0)
			child := f.access(issued.ID)
			d := child.Snapshot()
			if mode == "cookie" {
				if !issued.ExpiresAt.IsZero() || issued.ExpiresIn != 0 || !d.ExpiresAt.IsZero() || d.ParentID != "" {
					t.Fatalf("service token inherited cookie lifetime: %+v", issued)
				}
				if ok, previous := child.Match(issued.Token, f.now.AddDate(20, 0, 0)); !ok || previous {
					t.Fatal("nonexpiring service token expired with its login cookie")
				}
			} else if !d.ExpiresAt.Equal(f.access(f.actorID).Snapshot().ExpiresAt) || d.ParentID != f.actorID || issued.ExpiresIn != 3600 {
				t.Fatalf("delegated token escaped finite parent: %+v", d)
			}
		})
	}
}

func TestNonexpiringBearerParentCanDelegateAndRevoke(t *testing.T) {
	f := newNonexpiringFixture(t, "bearer", false)
	child := f.issue(f.actorID, 0)
	grandchild := f.issue(child.ID, 0)
	for _, id := range []string{child.ID, grandchild.ID} {
		d := f.access(id).Snapshot()
		if !d.ExpiresAt.IsZero() || !f.access(id).Valid(f.now.AddDate(20, 0, 0)) {
			t.Fatalf("nonexpiring delegation became finite: %+v", d)
		}
	}
	if err := f.manager.Revoke(context.Background(), f.guard(f.actorID), child.ID, 1); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{child.ID, grandchild.ID} {
		if f.access(id).Valid(f.now) {
			t.Fatal("nonexpiring descendant survived revocation", id)
		}
	}
}

func TestNonexpiringRenewalRespectsActorAndAncestorCaps(t *testing.T) {
	t.Run("self renewal can remove root expiry", func(t *testing.T) {
		f := newNonexpiringFixture(t, "bearer", true)
		view, err := f.manager.Renew(context.Background(), f.guard(f.actorID), f.actorID, authstate.RenewRequest{Revision: 1, TTLSeconds: 0})
		if err != nil || !view.ExpiresAt.IsZero() || !f.access(f.actorID).Valid(f.now.AddDate(20, 0, 0)) {
			t.Fatal("root could not become nonexpiring", view, err)
		}
	})
	t.Run("cookie can renew an independent service token without expiry", func(t *testing.T) {
		f := newNonexpiringFixture(t, "cookie", true)
		token := f.issue(f.actorID, 600)
		view, err := f.manager.Renew(context.Background(), f.guard(f.actorID), token.ID, authstate.RenewRequest{Revision: 1, TTLSeconds: 0})
		if err != nil || !view.ExpiresAt.IsZero() || !f.access(token.ID).Valid(f.now.AddDate(20, 0, 0)) {
			t.Fatal("service renewal inherited cookie expiry", view, err)
		}
	})
	t.Run("finite administrator still bounds renewal", func(t *testing.T) {
		f := newNonexpiringFixture(t, "bearer", true)
		token := f.issue(f.actorID, 600)
		view, err := f.manager.Renew(context.Background(), f.guard(f.actorID), token.ID, authstate.RenewRequest{Revision: 1, TTLSeconds: 0})
		if err != nil || !view.ExpiresAt.Equal(f.access(f.actorID).Snapshot().ExpiresAt) {
			t.Fatal("renewal escaped finite administrator", view, err)
		}
	})
	t.Run("self renewal cannot escape parent", func(t *testing.T) {
		f := newNonexpiringFixture(t, "bearer", true)
		token := f.issue(f.actorID, 600)
		view, err := f.manager.Renew(context.Background(), f.guard(token.ID), token.ID, authstate.RenewRequest{Revision: 1, TTLSeconds: 0})
		if err != nil || !view.ExpiresAt.Equal(f.access(f.actorID).Snapshot().ExpiresAt) {
			t.Fatal("renewal escaped finite ancestor", view, err)
		}
	})
	t.Run("nonexpiring administrator can remove child expiry", func(t *testing.T) {
		f := newNonexpiringFixture(t, "bearer", false)
		token := f.issue(f.actorID, 600)
		view, err := f.manager.Renew(context.Background(), f.guard(f.actorID), token.ID, authstate.RenewRequest{Revision: 1, TTLSeconds: 0})
		if err != nil || !view.ExpiresAt.IsZero() {
			t.Fatal("nonexpiring parent could not remove child expiry", view, err)
		}
	})
}

func TestNonexpiringRotationKeepsFinitePreviousSecretGrace(t *testing.T) {
	f := newNonexpiringFixture(t, "cookie", true)
	old := f.issue(f.actorID, 0)
	rotated, err := f.manager.Rotate(context.Background(), f.guard(f.actorID), old.ID, authstate.RotateRequest{Revision: 1, GraceSeconds: 60})
	if err != nil || !rotated.ExpiresAt.IsZero() || rotated.ExpiresIn != 0 {
		t.Fatal("rotation changed lifetime", rotated, err)
	}
	a := f.access(old.ID)
	if ok, previous := a.Match(old.Token, f.now.Add(59*time.Second)); !ok || !previous {
		t.Fatal("previous secret grace ended too early")
	}
	if ok, _ := a.Match(old.Token, f.now.Add(time.Minute)); ok {
		t.Fatal("previous secret remained valid after finite grace")
	}
	if ok, previous := a.Match(rotated.Token, f.now.AddDate(20, 0, 0)); !ok || previous {
		t.Fatal("current rotated secret expired")
	}
}

func TestNonexpiringDescendantsBecomeFiniteWhenParentShortens(t *testing.T) {
	f := newNonexpiringFixture(t, "bearer", false)
	child := f.issue(f.actorID, 0)
	grandchild := f.issue(child.ID, 0)
	view, err := f.manager.Renew(context.Background(), f.guard(f.actorID), f.actorID, authstate.RenewRequest{Revision: 1, TTLSeconds: 600})
	if err != nil {
		t.Fatal(err)
	}
	want := f.now.Add(10 * time.Minute)
	for _, id := range []string{f.actorID, child.ID, grandchild.ID} {
		d := f.access(id).Snapshot()
		if !d.ExpiresAt.Equal(want) || d.Revision != 2 || f.access(id).Valid(want) {
			t.Fatal("descendant retained unlimited lifetime", view, d)
		}
	}
}

func TestNonexpiringPruneAndBackupKeepLiveAccess(t *testing.T) {
	f := newNonexpiringFixture(t, "cookie", true)
	token := f.issue(f.actorID, 0)
	if removed, err := f.manager.Prune(context.Background(), f.now.Add(-24*time.Hour)); err != nil || removed != 0 {
		t.Fatal("prune deleted nonexpiring token", removed, err)
	}
	backup, err := f.repo.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(backup)
	if err != nil {
		t.Fatal(err)
	}
	var restored authstate.Backup
	if err = json.Unmarshal(raw, &restored); err != nil || authstate.ValidateBackup(restored) != nil {
		t.Fatal("nonexpiring backup was not valid", err)
	}
	repo := authstate.NewMemoryWithClock(func() time.Time { return f.now })
	t.Cleanup(func() { _ = repo.Close() })
	if err = repo.Import(context.Background(), restored); err != nil {
		t.Fatal(err)
	}
	a, _, err := repo.Get(context.Background(), token.ID)
	if err != nil || !a.Snapshot().ExpiresAt.IsZero() || !a.Valid(f.now.AddDate(20, 0, 0)) {
		t.Fatal("backup lost nonexpiring lifetime", err)
	}
}

func TestNonexpiringValidationRejectsUnlimitedCookieAndDelegationEscape(t *testing.T) {
	f := newNonexpiringFixture(t, "cookie", true)
	cookie := f.access(f.actorID).Snapshot()
	cookie.ExpiresAt = time.Time{}
	if authstate.ValidAccess(cookie) {
		t.Fatal("cookie accepted an unlimited session")
	}
	finite := newNonexpiringFixture(t, "bearer", true)
	child := finite.issue(finite.actorID, 600)
	backup, err := finite.repo.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i := range backup.Accesses {
		if backup.Accesses[i].ID == child.ID {
			backup.Accesses[i].ExpiresAt = time.Time{}
		}
	}
	if authstate.ValidateBackup(backup) == nil {
		t.Fatal("backup accepted nonexpiring child of finite parent")
	}
}

package statecontract

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
)

func runNonexpiringClock(t *testing.T, open func(*testing.T, func() time.Time) authstate.Repository) {
	t.Helper()
	t.Run("nonexpiring-persistence-rotation-and-revocation", func(t *testing.T) {
		f := newClockLifecycle(t, open)
		issued := issueNonexpiringFull(t, f, f.root)
		access := f.lookup(issued.ID)
		if !access.Snapshot().ExpiresAt.IsZero() || issued.ExpiresIn != 0 || access.Snapshot().ParentID != "" {
			t.Fatal("repository did not retain an independent nonexpiring token", access.View())
		}
		// SQL stores must retain the sentinel even after materializing a fresh
		// access model and serializing a backup for another repository.
		backup, err := f.repo.Export(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(backup)
		if err != nil {
			t.Fatal(err)
		}
		var restored authstate.Backup
		if err = json.Unmarshal(raw, &restored); err != nil {
			t.Fatal(err)
		}
		restoredRepo := open(t, f.now)
		t.Cleanup(func() { _ = restoredRepo.Close() })
		if err = restoredRepo.Import(f.ctx, restored); err != nil {
			t.Fatal("cannot restore nonexpiring access", err)
		}
		restoredAccess, _, err := restoredRepo.Get(f.ctx, issued.ID)
		if err != nil || !restoredAccess.Snapshot().ExpiresAt.IsZero() {
			t.Fatal("restored grant lost its unlimited lifetime", err)
		}
		f.set(f.base.Add(2 * time.Hour))
		if ok, previous := f.lookup(issued.ID).Match(issued.Token, f.now()); !ok || previous {
			t.Fatal("service token expired with the original cookie")
		}
		rotated, err := f.manager.Rotate(f.ctx, guard(access), issued.ID, authstate.RotateRequest{Revision: 1, GraceSeconds: 60})
		if err != nil || !rotated.ExpiresAt.IsZero() || rotated.ExpiresIn != 0 {
			t.Fatal("rotation failed or changed unlimited lifetime", rotated, err)
		}
		access = f.lookup(issued.ID)
		until := f.now().Add(time.Minute)
		if deadline, ok := access.DigestExpiry(authstate.Digest(issued.Token), f.now()); !ok || !deadline.Equal(until) {
			t.Fatal("previous secret does not have a finite grace deadline", deadline, ok)
		}
		f.set(until)
		if ok, _ := access.Match(issued.Token, f.now()); ok {
			t.Fatal("nonexpiring token retained its old secret beyond grace")
		}
		if ok, previous := access.Match(rotated.Token, f.now().AddDate(20, 0, 0)); !ok || previous {
			t.Fatal("current secret unexpectedly expires")
		}
		renewed, err := f.manager.Renew(f.ctx, guard(access), issued.ID, authstate.RenewRequest{Revision: 2, TTLSeconds: 0})
		if err != nil || !renewed.ExpiresAt.IsZero() {
			t.Fatal("self-renewal lost unlimited lifetime", renewed, err)
		}
		access = f.lookup(issued.ID)
		if count, err := f.manager.Prune(f.ctx, f.now().Add(-time.Minute)); err != nil || count != 1 {
			t.Fatal("retention must remove only the expired login cookie", count, err)
		}
		if !f.lookup(issued.ID).Valid(f.now()) {
			t.Fatal("retention removed live nonexpiring access")
		}
		if err = f.manager.Revoke(f.ctx, guard(access), issued.ID, 3); err != nil {
			t.Fatal(err)
		}
		access = f.lookup(issued.ID)
		if ok, _ := access.Match(rotated.Token, f.now()); ok {
			t.Fatal("revocation did not invalidate current secret")
		}
		if !access.Snapshot().ExpiresAt.Equal(f.now()) {
			t.Fatal("revoked unlimited grant has no finite retention deadline", access.View())
		}
		f.set(until.Add(time.Hour))
		if count, err := f.manager.Prune(f.ctx, until.Add(time.Nanosecond)); err != nil || count != 2 {
			t.Fatal("revoked grant and unreferenced connection were not pruned", count, err)
		}
	})
	t.Run("nonexpiring-parent-survives-revoked-descendant-retention", func(t *testing.T) {
		f := newClockLifecycle(t, open)
		root := issueNonexpiringFull(t, f, f.root)
		child := issueNonexpiringFull(t, f, f.lookup(root.ID))
		grandchild := issueNonexpiringFull(t, f, f.lookup(child.ID))
		f.set(f.base.Add(50 * time.Second))
		revokedAt := f.now()
		if err := f.manager.Revoke(f.ctx, guard(f.lookup(root.ID)), child.ID, 1); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{child.ID, grandchild.ID} {
			access := f.lookup(id)
			if access.Valid(f.now()) || !access.Snapshot().ExpiresAt.Equal(revokedAt) {
				t.Fatal("revoked unlimited descendant has no retention deadline", access.View())
			}
		}
		f.set(f.base.Add(2 * time.Hour))
		if count, err := f.manager.Prune(f.ctx, revokedAt.Add(time.Nanosecond)); err != nil || count != 2 {
			t.Fatal("retention did not remove exactly the revoked descendants", count, err)
		}
		if !f.lookup(root.ID).Valid(f.now()) || !f.lookup(root.ID).Snapshot().ExpiresAt.IsZero() {
			t.Fatal("pruning revoked descendants removed the active nonexpiring parent")
		}
	})
	t.Run("nonexpiring-descendants-clamp-atomically", func(t *testing.T) {
		f := newClockLifecycle(t, open)
		root := issueNonexpiringFull(t, f, f.root)
		f.set(f.base.Add(2 * time.Hour))
		child := issueNonexpiringFull(t, f, f.lookup(root.ID))
		grandchild := issueNonexpiringFull(t, f, f.lookup(child.ID))
		for _, id := range []string{root.ID, child.ID, grandchild.ID} {
			if !f.lookup(id).Snapshot().ExpiresAt.IsZero() {
				t.Fatal("unlimited ancestry acquired an expiry", id)
			}
		}
		view, err := f.manager.Renew(f.ctx, guard(f.lookup(root.ID)), root.ID, authstate.RenewRequest{Revision: 1, TTLSeconds: 600})
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{root.ID, child.ID, grandchild.ID} {
			access := f.lookup(id)
			if !access.Snapshot().ExpiresAt.Equal(view.ExpiresAt) || access.Snapshot().Revision != 2 || access.Valid(view.ExpiresAt) {
				t.Fatal("persisted descendant escaped its shortened parent", access.View())
			}
		}
		backup, err := f.repo.Export(f.ctx)
		if err != nil || authstate.ValidateBackup(backup) != nil {
			t.Fatal("cascade published invalid ancestry", err)
		}
	})
}

func issueNonexpiringFull(t *testing.T, f *clockLifecycle, actor authstate.Access) authstate.Issued {
	t.Helper()
	issued, err := f.manager.Issue(f.ctx, guard(actor), actor.Snapshot().ConnectionID, authstate.IssueRequest{TTLSeconds: 0, Policy: authstate.FullPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	return issued
}

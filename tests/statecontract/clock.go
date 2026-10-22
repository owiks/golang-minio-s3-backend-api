package statecontract

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/secretbox"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

// RunClock exercises exact lifetime boundaries against each repository. The
// historical clock makes any accidental wall-clock dependency fail immediately.
func RunClock(t *testing.T, open func(*testing.T, func() time.Time) authstate.Repository) {
	t.Helper()
	t.Run("rotation-and-expiry-boundaries", func(t *testing.T) {
		f := newClockLifecycle(t, open)
		issued := f.issue(120)
		if issued.ExpiresIn != 120 || !issued.ExpiresAt.Equal(f.base.Add(120*time.Second)) {
			t.Fatal("issued lifetime uses a different clock", issued)
		}
		f.set(f.base.Add(10 * time.Second))
		rotated, err := f.manager.Rotate(f.ctx, guard(f.root), issued.ID, authstate.RotateRequest{Revision: 1, GraceSeconds: 60})
		if err != nil || rotated.ExpiresIn != 110 {
			t.Fatal("rotation uses a different clock", rotated, err)
		}
		access := f.lookup(issued.ID)
		until := f.base.Add(70 * time.Second)
		if !access.Snapshot().PreviousUntil.Equal(until) {
			t.Fatal("grace period uses a different clock", access.View())
		}
		f.set(until.Add(-time.Nanosecond))
		if ok, previous := access.Match(issued.Token, f.now()); !ok || !previous {
			t.Fatal("previous token rejected before grace boundary")
		}
		if _, err = f.manager.Renew(f.ctx, authstate.CredentialGuard(access, true), issued.ID, authstate.RenewRequest{Revision: 2, TTLSeconds: 60}); apperror.Public(err).Status != 403 {
			t.Fatal("previous credential extended its own lifetime", err)
		}
		f.set(until)
		if ok, _ := access.Match(issued.Token, f.now()); ok {
			t.Fatal("previous token accepted at grace boundary")
		}
		if _, ok := access.DigestExpiry(authstate.Digest(issued.Token), f.now()); ok {
			t.Fatal("previous digest accepted at grace boundary")
		}
		if ok, previous := access.Match(rotated.Token, f.now()); !ok || previous {
			t.Fatal("current token invalidated at previous-token boundary")
		}
		f.set(f.base.Add(100 * time.Second))
		previousToken := rotated.Token
		rotated, err = f.manager.Rotate(f.ctx, guard(f.root), issued.ID, authstate.RotateRequest{Revision: 2, GraceSeconds: 60})
		if err != nil || rotated.ExpiresIn != 20 {
			t.Fatal("late rotation uses a different clock", rotated, err)
		}
		access = f.lookup(issued.ID)
		if !access.Snapshot().PreviousUntil.Equal(issued.ExpiresAt) {
			t.Fatal("grace exceeds the credential absolute lifetime")
		}
		if ok, _ := access.Match(issued.Token, f.now()); ok {
			t.Fatal("second rotation revived an older credential")
		}
		f.set(issued.ExpiresAt.Add(-time.Nanosecond))
		if ok, previous := access.Match(previousToken, f.now()); !ok || !previous {
			t.Fatal("clamped grace rejected the previous token before expiry")
		}
		if ok, _ := access.Match(rotated.Token, f.now()); !ok {
			t.Fatal("current token rejected before expiry")
		}
		f.set(issued.ExpiresAt)
		if access.Valid(f.now()) {
			t.Fatal("credential remains valid at expiry")
		}
		if _, err = f.manager.Renew(f.ctx, guard(access), issued.ID, authstate.RenewRequest{Revision: 3, TTLSeconds: 60}); apperror.Public(err).Status != 403 {
			t.Fatal("expired credential renewed itself", err)
		}
		if _, err = f.manager.Rotate(f.ctx, guard(f.root), issued.ID, authstate.RotateRequest{Revision: 3}); apperror.Public(err).Status != 409 {
			t.Fatal("expired target rotated", err)
		}
		// A previously authorized command must be rechecked under the repository
		// lock using its current clock, rather than the old audit timestamp.
		_, err = f.repo.Apply(f.ctx, authstate.Command{Kind: "logout", Guard: guard(access), TargetID: issued.ID, Revision: 3, Event: authstate.Event{ID: securetoken.New(), Owner: access.Snapshot().Owner, ActorID: issued.ID, TargetID: issued.ID, Action: "access.closed", At: f.base}})
		if apperror.Public(err).Status != 403 {
			t.Fatal("repository accepted an actor expired after authorization", err)
		}
		f.set(f.root.Snapshot().ExpiresAt)
		if _, err = f.manager.Connections(f.ctx, guard(f.root)); apperror.Public(err).Status != 403 {
			t.Fatal("manager accepted an actor at its exact expiry", err)
		}
	})
	t.Run("renewal-and-retention-boundaries", func(t *testing.T) {
		f := newClockLifecycle(t, open)
		issued := f.issue(120)
		f.set(f.base.Add(119 * time.Second))
		access := f.lookup(issued.ID)
		renewed, err := f.manager.Renew(f.ctx, guard(access), issued.ID, authstate.RenewRequest{Revision: 1, TTLSeconds: 60})
		if err != nil || !renewed.ExpiresAt.Equal(f.now().Add(time.Minute)) {
			t.Fatal("renewal uses a different clock", renewed, err)
		}
		events, err := f.repo.Events(f.ctx, f.root.Snapshot().Owner)
		if err != nil || len(events) != 3 || !events[0].At.Equal(f.now()) {
			t.Fatal("audit time differs from lifecycle clock", events, err)
		}
		f.set(f.base.Add(2 * time.Hour))
		for _, before := range []time.Time{f.now(), f.now().Add(time.Nanosecond)} {
			if _, err = f.manager.Prune(f.ctx, before); apperror.Public(err).Status != 400 {
				t.Fatal("manager accepted a current/future retention boundary", err)
			}
			if _, err = f.repo.Prune(f.ctx, before); apperror.Public(err).Status != 400 {
				t.Fatal("repository accepted a current/future retention boundary", err)
			}
		}
		if count, err := f.manager.Prune(f.ctx, renewed.ExpiresAt); err != nil || count != 0 {
			t.Fatal("credential deleted at its exact retention boundary", count, err)
		}
		if count, err := f.manager.Prune(f.ctx, renewed.ExpiresAt.Add(time.Nanosecond)); err != nil || count != 1 {
			t.Fatal("expired credential not pruned after retention boundary", count, err)
		}
		if _, _, err = f.manager.Lookup(f.ctx, issued.ID); !authstate.IsMissing(err) {
			t.Fatal("pruned credential remains readable", err)
		}
		if count, err := f.manager.Prune(f.ctx, f.root.Snapshot().ExpiresAt); err != nil || count != 0 {
			t.Fatal("root or referenced connection pruned too early", count, err)
		}
		if count, err := f.manager.Prune(f.ctx, f.root.Snapshot().ExpiresAt.Add(time.Nanosecond)); err != nil || count != 2 {
			t.Fatal("unreferenced expired root/connection not pruned", count, err)
		}
	})
}

type clockLifecycle struct {
	t       *testing.T
	ctx     context.Context
	base    time.Time
	clock   atomic.Int64
	repo    authstate.Repository
	manager *authstate.Manager
	root    authstate.Access
}

func newClockLifecycle(t *testing.T, open func(*testing.T, func() time.Time) authstate.Repository) *clockLifecycle {
	t.Helper()
	f := &clockLifecycle{t: t, ctx: context.Background(), base: time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)}
	f.set(f.base)
	f.repo = open(t, f.now)
	t.Cleanup(func() { _ = f.repo.Close() })
	box, err := secretbox.Ephemeral()
	if err != nil {
		t.Fatal(err)
	}
	f.manager = authstate.NewManagerWithClock(f.repo, box, time.Hour, f.now)
	rootID := securetoken.New()
	if err = f.manager.Bootstrap(f.ctx, authstate.Bootstrap{ID: rootID, ConnectionID: securetoken.New(), Endpoint: "https://s3.example.test", Mode: "cookie", Digest: authstate.Digest("fixture-cookie"), CSRFDigest: authstate.Digest("fixture-csrf"), ExpiresAt: f.base.Add(time.Hour), Secrets: authstate.Secrets{AccessKey: "fixture-key", SecretKey: "fixture-secret"}}); err != nil {
		t.Fatal(err)
	}
	f.root = f.lookup(rootID)
	return f
}

func (f *clockLifecycle) now() time.Time   { return time.Unix(0, f.clock.Load()).UTC() }
func (f *clockLifecycle) set(at time.Time) { f.clock.Store(at.UnixNano()) }
func (f *clockLifecycle) lookup(id string) authstate.Access {
	f.t.Helper()
	a, _, err := f.manager.Lookup(f.ctx, id)
	if err != nil {
		f.t.Fatal(err)
	}
	return a
}
func (f *clockLifecycle) issue(ttl int64) authstate.Issued {
	f.t.Helper()
	issued, err := f.manager.Issue(f.ctx, guard(f.root), f.root.Snapshot().ConnectionID, authstate.IssueRequest{TTLSeconds: ttl, Policy: authstate.Policy{Scopes: []string{"objects:read"}}})
	if err != nil {
		f.t.Fatal(err)
	}
	return issued
}

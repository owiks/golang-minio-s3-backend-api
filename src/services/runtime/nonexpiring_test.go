package runtime

import (
	"context"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/capabilities"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
)

func TestNonexpiringBearerSessionRetainsBoundedRuntime(t *testing.T) {
	endpoint, _ := url.Parse("http://fixture.example")
	tr := http.DefaultTransport.(*http.Transport).Clone()
	defer tr.CloseIdleConnections()
	cfg := config.Default()
	cfg.SessionTTL = 0
	s, _, err := NewBearerSession(Connection{AccessKey: "fixture", SecretKey: "fixture-secret"}, cfg, endpoint, endpoint, tr)
	if err != nil || !s.ExpiresAt().IsZero() || s.Expired(time.Now().AddDate(100, 0, 0)) {
		t.Fatal("permanent bearer was expired", err)
	}
	store := NewSessionStore()
	defer store.Close()
	if err = store.Put(s); err != nil {
		t.Fatal(err)
	}
	store.Reap(time.Now().AddDate(100, 0, 0))
	if !store.Has(s.SessionID()) {
		t.Fatal("permanent session removed from bounded cache")
	}
}

func TestNonexpiringNativeCredentialStillChecksRevocationAndCapturedDeadline(t *testing.T) {
	app, s, requests := nativeLifetimeFixture(t, 0)
	ctx := s.RequestContext(context.Background())
	if _, err := NewClients(s).ListBuckets(ctx); err != nil {
		t.Fatal("permanent credential cannot read", err)
	}
	if ttl, err := s.signPermission(ctx, "objects:read", "bucket", "key", time.Minute); err != nil || ttl != time.Minute {
		t.Fatal("permanent credential cannot sign finite links", ttl, err)
	}
	captured := authstate.WithCredential(ctx, s.SessionID(), s.Access().Snapshot().Digest, time.Now().Add(-time.Second))
	if _, err := NewClients(s).ListBuckets(captured); err == nil {
		t.Fatal("expired captured deadline bypassed")
	}
	if _, err := s.signPermission(captured, "objects:read", "bucket", "key", time.Minute); err == nil {
		t.Fatal("expired captured deadline could sign")
	}
	if err := app.Auth.Revoke(ctx, s.AccessGuard(), s.SessionID(), s.Access().Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := NewClients(s).ListBuckets(ctx); err == nil {
		t.Fatal("revoked permanent credential accepted")
	}
	if requests.Load() != 1 {
		t.Fatal("expired/revoked credential reached upstream", requests.Load())
	}
}

func TestNonexpiringCredentialRotationKeepsFiniteOverlap(t *testing.T) {
	app, old, _ := nativeLifetimeFixture(t, 0)
	ctx := old.RequestContext(context.Background())
	d := old.Access().Snapshot()
	if _, err := app.Auth.Rotate(ctx, old.AccessGuard(), d.ID, authstate.RotateRequest{Revision: d.Revision, GraceSeconds: 1}); err != nil {
		t.Fatal(err)
	}
	a, c, err := app.Auth.Lookup(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	overlap, err := app.BindAccess(ctx, a, c, true)
	if err != nil || overlap.ExpiresAt().IsZero() || !overlap.ExpiresAt().Equal(a.Snapshot().PreviousUntil) {
		t.Fatal("rotation overlap lost finite deadline", err)
	}
	fresh := rebindNativeSession(t, app, d.ID)
	if !fresh.ExpiresAt().IsZero() {
		t.Fatal("rotation introduced current credential expiration")
	}
	time.Sleep(time.Until(a.Snapshot().PreviousUntil) + time.Millisecond)
	if _, err = NewClients(fresh).ListBuckets(ctx); err == nil {
		t.Fatal("old digest survived finite overlap")
	}
	if _, err = NewClients(fresh).ListBuckets(fresh.RequestContext(context.Background())); err != nil {
		t.Fatal("current permanent credential stopped", err)
	}
}

func TestNonexpiringCapabilityViewStillExpiresEvidence(t *testing.T) {
	store := NewCapabilityStore()
	defer store.Close()
	v := capabilityTestView(store, "permanent", "i", "p", "1")
	v.expires = time.Time{}
	var count atomic.Int32
	load := capabilityLoader(&count)
	ctx := context.Background()
	scope := capabilities.Scope{Bucket: "bucket", Key: "key"}
	first, err := v.Resolve(ctx, capabilities.ObjectAttributes, scope, load)
	if err != nil || first.ExpiresAt.IsZero() {
		t.Fatal("permanent session lost finite evidence TTL", err)
	}
	if _, err = v.Resolve(ctx, capabilities.ObjectAttributes, scope, load); err != nil || count.Load() != 1 {
		t.Fatal("permanent session could not use cache", err)
	}
	store.now = func() time.Time { return first.ExpiresAt }
	if _, err = v.Resolve(ctx, capabilities.ObjectAttributes, scope, load); err != nil || count.Load() != 2 {
		t.Fatal("capability evidence did not expire", err)
	}
}

func TestNonexpiringOperationExecutorChecksCurrentAccess(t *testing.T) {
	app, s, _ := nativeLifetimeFixture(t, 0)
	executor := operationExecutor{app}
	job := operations.Snapshot{Scope: operations.Scope{ActorID: s.SessionID(), ConnectionID: s.ConnectionID()}, Spec: operations.Spec{Scan: &operations.ScanSpec{Bucket: "bucket"}}}
	if err := executor.Authorize(context.Background(), job); err != nil {
		t.Fatal("permanent operation rejected", err)
	}
	if err := app.Auth.Revoke(context.Background(), s.AccessGuard(), s.SessionID(), s.Access().Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	if err := executor.Authorize(context.Background(), job); err == nil {
		t.Fatal("permanent operation ignored revocation")
	}
}

func TestRemovingExpiryDoesNotExtendCapturedNativeRequest(t *testing.T) {
	app, old, requests := nativeLifetimeFixture(t, time.Second)
	captured := old.RequestContext(context.Background())
	d := old.Access().Snapshot()
	view, err := app.Auth.Renew(context.Background(), old.AccessGuard(), d.ID, authstate.RenewRequest{Revision: d.Revision, TTLSeconds: 0})
	if err != nil || !view.ExpiresAt.IsZero() {
		t.Fatal("root could not become permanent", err)
	}
	fresh := rebindNativeSession(t, app, d.ID)
	time.Sleep(time.Until(d.ExpiresAt) + time.Millisecond)
	if _, err = NewClients(old).ListBuckets(context.Background()); err == nil {
		t.Fatal("permanent renewal extended old client")
	}
	if _, err = NewClients(fresh).ListBuckets(captured); err == nil {
		t.Fatal("permanent renewal extended captured request")
	}
	if _, err = NewClients(fresh).ListBuckets(fresh.RequestContext(context.Background())); err != nil {
		t.Fatal("new permanent credential could not read", err)
	}
	if requests.Load() != 1 {
		t.Fatal("expired captured request reached upstream", requests.Load())
	}
}

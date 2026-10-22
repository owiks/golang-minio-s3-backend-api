package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

func nativeLifetimeFixture(t *testing.T, ttl time.Duration) (*State, *Session, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<ListAllMyBucketsResult><Buckets></Buckets></ListAllMyBucketsResult>`)
	}))
	t.Cleanup(upstream.Close)
	cfg := config.Default()
	cfg.Endpoints.Allowed[upstream.URL] = true
	app := New(cfg)
	t.Cleanup(app.Close)
	id, cid := securetoken.New(), securetoken.New()
	ctx := context.Background()
	if err := app.Auth.Bootstrap(ctx, authstate.Bootstrap{ID: id, ConnectionID: cid, Endpoint: upstream.URL, Region: "us-east-1", Mode: "bearer", Digest: authstate.Digest("native-lifetime-token"), ExpiresAt: time.Now().Add(ttl), Secrets: authstate.Secrets{AccessKey: "fixture-access", SecretKey: "fixture-secret"}}); err != nil {
		t.Fatal(err)
	}
	access, connection, err := app.Auth.Lookup(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	s, err := app.BindAccess(ctx, access, connection, false)
	if err != nil {
		t.Fatal(err)
	}
	return app, s, &requests
}

func rebindNativeSession(t *testing.T, app *State, id string) *Session {
	t.Helper()
	a, c, err := app.Auth.Lookup(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	s, err := app.BindAccess(context.Background(), a, c, false)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRetainedNativeClientsRejectRevokedBearerSecret(t *testing.T) {
	app, old, requests := nativeLifetimeFixture(t, time.Hour)
	ctx := old.RequestContext(context.Background())
	if _, err := NewClients(old).ListBuckets(ctx); err != nil {
		t.Fatal(err)
	}
	a := old.Access().Snapshot()
	if _, err := app.Auth.Rotate(ctx, old.AccessGuard(), a.ID, authstate.RotateRequest{Revision: a.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewClients(old).ListBuckets(ctx); err == nil {
		t.Fatal("retained native client accepted the replaced bearer digest")
	}
	fresh := rebindNativeSession(t, app, old.SessionID())
	if _, err := NewClients(fresh).ListBuckets(ctx); err == nil {
		t.Fatal("fresh native client accepted the captured old bearer digest")
	}
	if _, err := NewClients(fresh).ListBuckets(fresh.RequestContext(context.Background())); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatal("replaced bearer credential reached upstream", requests.Load())
	}
}

func TestRetainedNativeClientsRespectBearerGraceDeadline(t *testing.T) {
	app, old, requests := nativeLifetimeFixture(t, time.Hour)
	ctx := old.RequestContext(context.Background())
	a := old.Access().Snapshot()
	if _, err := app.Auth.Rotate(ctx, old.AccessGuard(), a.ID, authstate.RotateRequest{Revision: a.Revision, GraceSeconds: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewClients(old).ListBuckets(ctx); err != nil {
		t.Fatal("overlap credential stopped before its deadline", err)
	}
	current, _, err := app.Auth.Lookup(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Until(current.Snapshot().PreviousUntil) + time.Millisecond)
	if _, err = NewClients(old).ListBuckets(context.Background()); err == nil {
		t.Fatal("retained client ignored the persisted overlap deadline")
	}
	fresh := rebindNativeSession(t, app, a.ID)
	if _, err = NewClients(fresh).ListBuckets(ctx); err == nil {
		t.Fatal("captured overlap credential outlived its deadline on a fresh client")
	}
	if _, err = NewClients(fresh).ListBuckets(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatal("expired overlap credential reached upstream", requests.Load())
	}
}

func TestDelegatedRenewalDoesNotProlongCapturedNativeRequests(t *testing.T) {
	app, parent, requests := nativeLifetimeFixture(t, 30*time.Second)
	ctx := context.Background()
	issued, err := app.Auth.Issue(ctx, parent.AccessGuard(), parent.ConnectionID(), authstate.IssueRequest{TTLSeconds: 1, Policy: authstate.Policy{Scopes: []string{"objects:read", "buckets:read"}}})
	if err != nil {
		t.Fatal(err)
	}
	old := rebindNativeSession(t, app, issued.ID)
	captured := old.RequestContext(ctx)
	before := old.Access().Snapshot()
	view, err := app.Auth.Renew(ctx, old.AccessGuard(), issued.ID, authstate.RenewRequest{Revision: before.Revision, TTLSeconds: 60})
	if err != nil {
		t.Fatal("scoped delegate could not renew itself", err)
	}
	if !view.ExpiresAt.After(before.ExpiresAt) || view.ExpiresAt.After(parent.ExpiresAt()) {
		t.Fatal("delegated renewal did not preserve the parent lifetime cap")
	}
	fresh := rebindNativeSession(t, app, issued.ID)
	if _, err = NewClients(old).ListBuckets(captured); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Until(before.ExpiresAt) + time.Millisecond)
	if _, err = NewClients(old).ListBuckets(ctx); err == nil {
		t.Fatal("renewal extended an old bound operation")
	}
	if _, err = NewClients(fresh).ListBuckets(captured); err == nil {
		t.Fatal("renewal extended a captured request using a fresh client")
	}
	if _, err = NewClients(fresh).PresignedGetObject(captured, "fixture-bucket", "file.txt", time.Minute, nil); err == nil {
		t.Fatal("renewal extended a captured request's signing authority")
	}
	if _, err = NewClients(fresh).ListBuckets(fresh.RequestContext(ctx)); err != nil {
		t.Fatal("renewed credential did not work on a new operation", err)
	}
	if requests.Load() != 2 {
		t.Fatal("expired captured request reached upstream", requests.Load())
	}
}

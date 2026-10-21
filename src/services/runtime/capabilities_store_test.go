package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"minio-web/api/src/config"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/capabilities"
)

func capabilityTestView(store *CapabilityStore, session, internal, public, revision string) *capabilityView {
	return &capabilityView{store: store, owner: capabilityOwner{session: session, internal: internal, public: public, revision: revision}, expires: time.Now().Add(time.Hour)}
}
func capabilityLoader(count *atomic.Int32) func(context.Context) (capabilities.Result, error) {
	return func(context.Context) (capabilities.Result, error) {
		count.Add(1)
		return capabilities.Outcome(capabilities.ObjectAttributes, capabilities.Scope{Bucket: "bucket", Key: "key"}, "read_probe", nil, time.Now()), nil
	}
}

func TestCapabilityCacheIsolatesSessionEndpointsScopeAndIAMRevision(t *testing.T) {
	s := NewCapabilityStore()
	defer s.Close()
	var count atomic.Int32
	for _, owner := range []capabilityOwner{
		{session: "a", internal: "internal", public: "public", revision: "1"},
		{session: "b", internal: "internal", public: "public", revision: "1"},
		{session: "a", internal: "different", public: "public", revision: "1"},
		{session: "a", internal: "internal", public: "different", revision: "1"},
		{session: "a", internal: "internal", public: "public", revision: "2"},
	} {
		v := &capabilityView{store: s, owner: owner, expires: time.Now().Add(time.Hour)}
		for range 2 {
			if _, err := v.Resolve(context.Background(), capabilities.ObjectAttributes, capabilities.Scope{Bucket: "bucket", Key: "key"}, capabilityLoader(&count)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if count.Load() != 5 {
		t.Fatal("evidence crossed owner scope", count.Load())
	}
	v := capabilityTestView(s, "a", "internal", "public", "1")
	if _, err := v.Resolve(context.Background(), capabilities.ObjectAttributes, capabilities.Scope{Bucket: "bucket", Key: "other"}, capabilityLoader(&count)); err != nil {
		t.Fatal(err)
	}
	if count.Load() != 6 {
		t.Fatal("evidence crossed object scope")
	}
}

func TestCapabilityObservationExpiresAndNeverCrossesUsers(t *testing.T) {
	s := NewCapabilityStore()
	defer s.Close()
	now := time.Now()
	s.now = func() time.Time { return now }
	a := capabilityTestView(s, "a", "internal", "public", "1")
	scope := capabilities.Scope{Bucket: "bucket", Key: "key"}
	a.Observe(context.Background(), capabilities.Observation{ID: capabilities.ObjectAttributes, Scope: scope, Err: minio.ErrorResponse{Code: "AccessDenied"}})
	var count atomic.Int32
	r, err := a.Resolve(context.Background(), capabilities.ObjectAttributes, scope, capabilityLoader(&count))
	if err != nil || r.Status != capabilities.AccessDenied || r.Evidence != "observed_operation" || count.Load() != 0 {
		t.Fatal(r, err, count.Load())
	}
	b := capabilityTestView(s, "b", "internal", "public", "1")
	r, err = b.Resolve(context.Background(), capabilities.ObjectAttributes, scope, capabilityLoader(&count))
	if err != nil || r.Status != capabilities.Supported || count.Load() != 1 {
		t.Fatal(r, err)
	}
	now = now.Add(31 * time.Second)
	if _, err := a.Resolve(context.Background(), capabilities.ObjectAttributes, scope, capabilityLoader(&count)); err != nil {
		t.Fatal(err)
	}
	if count.Load() != 2 {
		t.Fatal("denial outlived TTL")
	}
	s.RemoveSession("a")
	if len(s.entries) != 1 {
		t.Fatal("logout retained session evidence", len(s.entries))
	}
	s.Reap(now.Add(time.Hour))
	if len(s.entries) != 0 {
		t.Fatal("expired evidence retained")
	}
}

func TestCapabilityCacheCoalescesAndWaitIsCancelable(t *testing.T) {
	s := NewCapabilityStore()
	defer s.Close()
	v := capabilityTestView(s, "a", "i", "p", "1")
	scope := capabilities.Scope{Bucket: "bucket", Key: "key"}
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	load := func(context.Context) (capabilities.Result, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return capabilities.Outcome(capabilities.ObjectAttributes, scope, "read_probe", nil, time.Now()), nil
	}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	wg.Go(func() {
		_, err := v.Resolve(context.Background(), capabilities.ObjectAttributes, scope, load)
		errs <- err
	})
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := v.Resolve(ctx, capabilities.ObjectAttributes, scope, load); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for range 19 {
		wg.Go(func() {
			_, err := v.Resolve(context.Background(), capabilities.ObjectAttributes, scope, load)
			errs <- err
		})
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("concurrent probe was duplicated", calls.Load())
	}
}

func TestCapabilityCacheBoundsEntriesAndInflight(t *testing.T) {
	s := NewCapabilityStore()
	defer s.Close()
	v := capabilityTestView(s, "a", "i", "p", "1")
	for n := range capabilityCacheLimit + 5 {
		v.Observe(context.Background(), capabilities.Observation{ID: capabilities.MultipartCreate, Scope: capabilities.Scope{Bucket: "bucket", Key: fmt.Sprintf("key-%d", n)}})
	}
	if len(s.entries) != capabilityCacheLimit {
		t.Fatal("unbounded cache", len(s.entries))
	}
	entered, release := make(chan struct{}, capabilityProbeLimit), make(chan struct{})
	load := func(context.Context) (capabilities.Result, error) {
		entered <- struct{}{}
		<-release
		return capabilities.Result{}, nil
	}
	var wg sync.WaitGroup
	for n := range capabilityProbeLimit {
		wg.Go(func() {
			_, _ = v.Resolve(context.Background(), capabilities.ObjectAttributes, capabilities.Scope{Bucket: "bucket", Key: fmt.Sprintf("probe-%d", n)}, load)
		})
	}
	for range capabilityProbeLimit {
		<-entered
	}
	_, err := v.Resolve(context.Background(), capabilities.ObjectAttributes, capabilities.Scope{Bucket: "bucket", Key: "excess"}, load)
	if apperror.Public(err).Code != "CapabilityBusy" {
		t.Fatal("unbounded concurrent probes", err)
	}
	close(release)
	wg.Wait()
	if len(s.flights) != 0 || len(s.active) != 0 {
		t.Fatal("flight resources retained")
	}
}

func TestCapabilityCanceledFailureNotCachedAndLogoutCannotRepopulate(t *testing.T) {
	s := NewCapabilityStore()
	defer s.Close()
	v := capabilityTestView(s, "a", "i", "p", "1")
	scope := capabilities.Scope{Bucket: "bucket", Key: "key"}
	ctx, cancel := context.WithCancel(context.Background())
	_, err := v.Resolve(ctx, capabilities.ObjectAttributes, scope, func(context.Context) (capabilities.Result, error) { cancel(); return capabilities.Result{}, nil })
	if !errors.Is(err, context.Canceled) || len(s.entries) != 0 {
		t.Fatal("canceled load cached", err)
	}
	alive := true
	v.alive = func() bool { return alive }
	_, err = v.Resolve(context.Background(), capabilities.ObjectAttributes, scope, func(context.Context) (capabilities.Result, error) {
		alive = false
		return capabilities.Outcome(capabilities.ObjectAttributes, scope, "read_probe", nil, time.Now()), nil
	})
	if err != nil || len(s.entries) != 0 {
		t.Fatal("logout repopulated cache", err)
	}
	v.Observe(context.Background(), capabilities.Observation{ID: capabilities.ObjectAttributes, Scope: scope})
	if len(s.entries) != 0 {
		t.Fatal("late observation repopulated logout")
	}
}

func TestCapabilityCacheUsesLiveIAMRevisionAndLogout(t *testing.T) {
	app := New(config.Config{})
	defer app.Close()
	u, _ := url.Parse("https://fixture.invalid")
	session := &Session{id: "fixture-session", internal: u, public: u, expires: time.Now().Add(time.Hour)}
	if err := app.StoreSession(session); err != nil {
		t.Fatal(err)
	}
	scope := capabilities.Scope{Bucket: "bucket", Key: "key"}
	app.CapabilityObserver(session).Observe(context.Background(), capabilities.Observation{ID: capabilities.ObjectAttributes, Scope: scope, Err: minio.ErrorResponse{Code: "AccessDenied"}})
	var count atomic.Int32
	result, err := app.CapabilityCache(session).Resolve(context.Background(), capabilities.ObjectAttributes, scope, capabilityLoader(&count))
	if err != nil || result.Status != capabilities.AccessDenied || count.Load() != 0 {
		t.Fatal(result, err, count.Load())
	}
	app.InvalidateAccess(session)
	result, err = app.CapabilityCache(session).Resolve(context.Background(), capabilities.ObjectAttributes, scope, capabilityLoader(&count))
	if err != nil || result.Status != capabilities.Supported || count.Load() != 1 {
		t.Fatal("IAM revision retained denial", result, err, count.Load())
	}
	old := app.CapabilityObserver(session)
	app.RemoveSession(session.id)
	old.Observe(context.Background(), capabilities.Observation{ID: capabilities.ObjectAttributes, Scope: scope})
	if len(app.Capabilities.entries) != 0 {
		t.Fatal("logout retained or repopulated evidence")
	}
}

func TestConfiguredConditionalCapabilityUsesSessionAssertionWithoutNetwork(t *testing.T) {
	u, _ := url.Parse("https://s3.fixture.invalid")
	for _, configured := range []bool{false, true} {
		cfg := config.Config{ConditionalCompleteEndpoints: map[string]bool{u.String(): configured}}.WithDefaults()
		app := New(cfg)
		session, err := NewConnectedSession(Connection{AccessKey: "fixture-access", SecretKey: "fixture-secret"}, app.Config(), u, u, &http.Transport{})
		if err != nil {
			app.Close()
			t.Fatal(err)
		}
		if err := app.StoreSession(session); err != nil {
			app.Close()
			t.Fatal(err)
		}
		value, err := capabilities.Report(context.Background(), NewClients(session), app.CapabilityCache(session), capabilities.Request{Feature: capabilities.ConditionalComplete})
		app.Close()
		if err != nil || len(value.Capabilities) != 1 {
			t.Fatal(value, err)
		}
		result := value.Capabilities[0]
		if result.Access != capabilities.AccessUnknown {
			t.Fatal("configuration claimed actor permission", result)
		}
		if configured {
			if result.Status != capabilities.Supported || result.Evidence != "confirmed_configuration" || result.Reason != "EndpointConfigured" {
				t.Fatal(result)
			}
		} else if result.Status != capabilities.Unknown {
			t.Fatal(result)
		}
	}
}

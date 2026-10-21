package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apihttp "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/presentation"
	accesssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/access"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/iam"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func TestAccessDiscoveryCancelledWaitReturnsBeforeOwnerUnlocks(t *testing.T) {
	var requests atomic.Int32
	s := actionCheckSession(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(500)
	})
	client := runtimesvc.NewClients(s)
	unlock, err := client.LockAccess(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() { once.Do(unlock) }
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := accesssvc.DiscoverAccess(ctx, runtimesvc.NewClients(s))
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("request cancellation lost", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("cancelled discovery still waits for the access lock owner")
		release()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("discovery did not stop after cleanup")
		}
	}
	if requests.Load() != 0 {
		t.Fatal("cancelled discovery reached MinIO", requests.Load())
	}
}

func TestAccessHTTPDeadlineWhileQueuedPreservesPolicyAndRevision(t *testing.T) {
	var requests atomic.Int32
	s := actionCheckSession(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		httpcommon.WriteJSON(w, r, 200, map[string]any{"AccountName": "fixture-user", "Policy": actionCheckPolicy})
	})
	client := runtimesvc.NewClients(s)
	if issue := client.RefreshActionPolicy(context.Background()); issue != nil {
		t.Fatal("initial action policy unavailable", issue)
	}
	requests.Store(0)
	query := iam.Query{Action: "admin:CreateUser"}
	if !s.Actions().Known() || !s.Actions().Allows(query) {
		t.Fatal("initial policy not published")
	}
	owner, err := client.LockAccess(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer owner()
	app := runtimesvc.New(testConfig())
	defer app.Close()
	if err := app.StoreSession(s); err != nil {
		t.Fatal(err)
	}
	revision := app.AccessRevision(s)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	r := httptest.NewRequest("GET", "http://ui.example/api/sessions/"+s.SessionID()+"/access", nil).WithContext(ctx)
	r.Header.Set("Origin", "http://ui.example")
	r.AddCookie(httpcommon.SessionCookie(s, r, false))
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		apihttp.NewHandler(app).ServeHTTP(w, r)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		owner()
		<-done
		t.Fatal("HTTP deadline still waits for the access lock owner")
	}
	var result struct{ Error presentation.Error }
	if w.Code != 504 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Error.Code != "Timeout" {
		t.Fatal("cancelled queue returned a successful snapshot or wrong error", w.Code, w.Body.String())
	}
	if requests.Load() != 0 || !s.Actions().Known() || !s.Actions().Allows(query) || s.Actions().Issue() != nil || app.AccessRevision(s) != revision {
		t.Fatal("queued deadline probed MinIO or replaced the existing policy/revision", requests.Load())
	}
	owner()
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	release, err := client.LockAccess(ctx)
	if err != nil {
		t.Fatal("HTTP cancellation leaked ownership", err)
	}
	release()
}

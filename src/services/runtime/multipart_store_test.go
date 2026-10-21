package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"minio-web/api/src/config"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/uploads/multipart"
)

func multipartStoreFixture(t *testing.T) (*State, *Session, *atomic.Int32, *atomic.Bool) {
	t.Helper()
	aborts := &atomic.Int32{}
	fail := &atomic.Bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "HEAD":
			w.WriteHeader(404)
		case r.Method == "POST" && r.URL.Query().Has("uploads"):
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<InitiateMultipartUploadResult><Bucket>fixture-bucket</Bucket><Key>file.bin</Key><UploadId>native-id</UploadId></InitiateMultipartUploadResult>`))
		case r.Method == "DELETE":
			aborts.Add(1)
			if fail.Load() {
				w.WriteHeader(503)
				_, _ = w.Write([]byte(`<Error><Code>ServiceUnavailable</Code></Error>`))
			} else {
				w.WriteHeader(204)
			}
		default:
			t.Errorf("unexpected %s", r.Method)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(server.Close)
	endpoint, _ := url.Parse(server.URL)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(tr.CloseIdleConnections)
	cfg := config.Config{SessionTTL: time.Hour, ConditionalCompleteEndpoints: map[string]bool{server.URL: true}}
	s, err := NewConnectedSession(Connection{AccessKey: "fixture", SecretKey: "fixture-secret", Region: "us-east-1"}, cfg, endpoint, endpoint, tr)
	if err != nil {
		t.Fatal(err)
	}
	app := New(cfg)
	if err = app.StoreSession(s); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	return app, s, aborts, fail
}

func createManaged(t *testing.T, app *State, s *Session) multipart.Snapshot {
	t.Helper()
	v, err := multipart.Create(context.Background(), app.Multipart, app.MultipartClients(s), multipart.CreateRequest{Bucket: "fixture-bucket", Key: "file.bin", Revision: "absent", Size: 1})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestMultipartStoreLogoutRevokesAdmissionAndRetriesCleanup(t *testing.T) {
	app, s, aborts, fail := multipartStoreFixture(t)
	v := createManaged(t, app, s)
	fail.Store(true)
	app.RemoveSession(s.SessionID())
	if app.Multipart.Get(v.ID) != nil {
		t.Fatal("removed owner retained operation access")
	}
	if aborts.Load() != 1 {
		t.Fatal("initial cleanup", aborts.Load())
	}
	// Failed logout cleanup is eligible by its retry schedule, not by the
	// original one-hour session deadline.
	fail.Store(false)
	app.Multipart.Reap(time.Now().Add(2 * time.Minute))
	if aborts.Load() != 2 {
		t.Fatal("logout residue not retried", aborts.Load())
	}
	app.Multipart.mu.Lock()
	count := len(app.Multipart.items)
	app.Multipart.mu.Unlock()
	if count != 0 {
		t.Fatal("confirmed cleanup retained operation")
	}
	_, err := multipart.Create(context.Background(), app.Multipart, app.MultipartClients(s), multipart.CreateRequest{Bucket: "fixture-bucket", Key: "file.bin", Revision: "absent", Size: 1})
	if err == nil || apperror.Public(err).Code != "SessionExpired" {
		t.Fatal("late create published", err)
	}
}

func TestMultipartStoreCompletedOperationsDoNotConsumeActiveSlots(t *testing.T) {
	app, s, _, _ := multipartStoreFixture(t)
	for i := 0; i < 20; i++ {
		v := createManaged(t, app, s)
		if _, err := multipart.Abort(context.Background(), app.Multipart, app.MultipartClients(s), v.ID); err != nil {
			t.Fatal(i, err)
		}
	}
}

func TestMultipartStoreCloseRejectsLatePublicationAndReads(t *testing.T) {
	app, s, _, _ := multipartStoreFixture(t)
	v := createManaged(t, app, s)
	app.Multipart.Close()
	if app.Multipart.Get(v.ID) != nil {
		t.Fatal("closed registry exposed operation")
	}
	_, err := multipart.Create(context.Background(), app.Multipart, app.MultipartClients(s), multipart.CreateRequest{Bucket: "fixture-bucket", Key: "file.bin", Revision: "absent", Size: 1})
	if err == nil || apperror.Public(err).Code != "ServerClosing" {
		t.Fatal(err)
	}
}

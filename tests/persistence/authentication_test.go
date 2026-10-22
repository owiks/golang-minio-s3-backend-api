package persistence_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	apihttp "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/secretbox"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/archives/plan"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
	"github.com/owiks/golang-minio-s3-backend-api/tests/databasefixtures"
	"github.com/owiks/golang-minio-s3-backend-api/tests/statefixtures"
)

func TestPersistentAuthenticationFailsClosed(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); http.Error(w, "not expected", 500) }))
	defer upstream.Close()
	cfg := config.Default()
	cfg.State = databasefixtures.Database(t, "sqlite")
	statefixtures.Migrate(t, cfg.State)
	ring, _ := secretbox.Generate()
	raw, _ := json.Marshal(ring)
	cfg.State.KeyringFile = filepath.Join(t.TempDir(), "keyring.json")
	if err := os.WriteFile(cfg.State.KeyringFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	m, err := persistence.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	id, secret := securetoken.New(), securetoken.New()
	if err = m.Bootstrap(context.Background(), authstate.Bootstrap{ID: id, ConnectionID: securetoken.New(), Endpoint: upstream.URL, Region: "us-east-1", Mode: "bearer", Digest: authstate.Digest(secret), ExpiresAt: time.Now().Add(time.Hour), Secrets: authstate.Secrets{AccessKey: "fixture", SecretKey: "fixture-secret"}}); err != nil {
		t.Fatal(err)
	}
	_ = m.Close()
	// Network policy is checked again, even for a credential issued before restart.
	m, err = persistence.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	app := runtime.NewWithAuth(cfg, m)
	handler := apihttp.NewHandler(app)
	request := func(language string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://api.test/api/sessions/"+id+"/credential", nil)
		r.Header.Set("Authorization", "Bearer "+secret)
		r.Header.Set("Accept-Language", language)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request("en"); w.Code == 200 {
		t.Fatal("rehydration bypassed changed network policy")
	}
	if calls.Load() != 0 {
		t.Fatal("disallowed endpoint was contacted")
	}
	app.Close()
	// A permitted rehydration constructs clients without any upstream request.
	cfg.Endpoints.Allowed[upstream.URL] = true
	m, err = persistence.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	app = runtime.NewWithAuth(cfg, m)
	defer app.Close()
	handler = apihttp.NewHandler(app)
	if w := request("en"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if calls.Load() != 0 {
		t.Fatal("credential metadata performed native I/O")
	}
	plan := archiveplan.New(archiveplan.Config{SessionID: id, Bucket: "data", Name: "file.zip", Created: time.Now()})
	ticket, err := app.Archives.Put(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	_ = m.Close() // Cached clients must not bypass an unavailable auth repository.
	app.Archives.Reap(time.Now())
	if app.Archives.Get(ticket, id) == nil || plan.Snapshot(ticket).Cancelled {
		t.Fatal("temporary DB failure was treated as confirmed revocation")
	}
	en, ru := request("en"), request("ru")
	if en.Code != 503 || ru.Code != 503 || en.Header().Get("Content-Language") != "en" || ru.Header().Get("Content-Language") != "ru" || en.Body.String() == ru.Body.String() {
		t.Fatal("closed repository or localization behavior invalid")
	}
	for _, w := range []*httptest.ResponseRecorder{en, ru} {
		if strings.Contains(w.Body.String(), secret) || strings.Contains(w.Body.String(), "fixture-secret") || strings.Contains(w.Body.String(), "AuthStateUnavailable") {
			t.Fatal("internal identifier or secret exposed")
		}
	}
}

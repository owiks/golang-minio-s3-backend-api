package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

type readinessRepository struct {
	authstate.Repository
	err error
}

func (r *readinessRepository) Ping(context.Context) error { return r.err }

func TestReadinessIsPublicAndSanitizesStoreFailure(t *testing.T) {
	store := &readinessRepository{Repository: authstate.NewMemory()}
	app := runtime.NewWithAuth(config.Default(), authstate.NewManager(store, nil, time.Hour))
	defer app.Close()
	handler := NewHandler(app)
	for _, language := range []string{"en", "ru"} {
		store.err = nil
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/ready", nil)
		r.Header.Set("Accept-Language", language)
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK || w.Body.String() != "{\"ok\":true}\n" {
			t.Fatalf("ready API failed: status=%d body=%s", w.Code, w.Body.String())
		}
		store.err = errors.New("mysql://secret-user:secret-password@internal-host/private-db")
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"code":"StateUnavailable"`) {
			t.Fatalf("unavailable store result: status=%d body=%s", w.Code, w.Body.String())
		}
		for _, secret := range []string{"mysql://", "secret-user", "secret-password", "internal-host", "private-db"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("readiness exposed store connection details")
			}
		}
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
		if w.Code != http.StatusOK || w.Body.String() != "{\"ok\":true}\n" {
			t.Fatalf("liveness depends on the state store: status=%d body=%s", w.Code, w.Body.String())
		}
	}
}

func TestMemoryStoreReadiness(t *testing.T) {
	app := runtime.New(config.Default())
	defer app.Close()
	handler := NewHandler(app)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/ready", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("open memory store is not ready: %d %s", w.Code, w.Body.String())
	}
	if err := app.Auth.Close(); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/ready", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("closed memory store is ready: %d %s", w.Code, w.Body.String())
	}
}

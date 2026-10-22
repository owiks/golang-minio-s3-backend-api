package status

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
)

func TestReadyBoundsStateCheck(t *testing.T) {
	w := httptest.NewRecorder()
	called := false
	err := Ready(w, httptest.NewRequest("GET", "/ready", nil), func(ctx context.Context) error {
		called = true
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Second || time.Until(deadline) <= 0 {
			t.Fatal("readiness check is missing its bounded deadline")
		}
		return nil
	})
	if err != nil || !called || w.Code != http.StatusOK || w.Body.String() != "{\"ok\":true}\n" {
		t.Fatalf("unexpected readiness result: status=%d body=%s error=%v", w.Code, w.Body.String(), err)
	}
}

func TestReadySanitizesStoreError(t *testing.T) {
	w := httptest.NewRecorder()
	err := Ready(w, httptest.NewRequest("GET", "/ready", nil), func(context.Context) error {
		return errors.New("postgres://secret-user:secret-password@internal-host/private-db")
	})
	if public := apperror.Public(err); public.Status != http.StatusServiceUnavailable || public.Code != "StateUnavailable" {
		t.Fatalf("unexpected readiness error: %#v", public)
	}
	if w.Body.Len() != 0 {
		t.Fatal("failed readiness wrote a success body")
	}
}

func TestReadyPreservesRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := httptest.NewRecorder()
	err := Ready(w, httptest.NewRequest("GET", "/ready", nil).WithContext(ctx), func(check context.Context) error {
		if !errors.Is(check.Err(), context.Canceled) {
			t.Fatal("readiness detached from the canceled request")
		}
		return nil
	})
	if public := apperror.Public(err); public.Code != "StateUnavailable" {
		t.Fatalf("canceled readiness reported success: %#v", public)
	}
}

func TestReadyHonorsEarlierRequestDeadline(t *testing.T) {
	deadline := time.Now().Add(100 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	err := Ready(httptest.NewRecorder(), httptest.NewRequest("GET", "/ready", nil).WithContext(ctx), func(check context.Context) error {
		got, ok := check.Deadline()
		if !ok || !got.Equal(deadline) {
			t.Fatal("readiness extended the request deadline")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

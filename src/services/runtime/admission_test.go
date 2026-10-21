package runtime

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
)

func TestLoginAdmissionAndTransfers(t *testing.T) {
	cfg := config.Default()
	cfg.Limits.LoginBurst = 1
	cfg.Limits.LoginInterval = time.Minute
	app := New(cfg)
	done, _, err := app.AdmitLogin("192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	done()
	if _, retry, err := app.AdmitLogin("192.0.2.1"); apperror.Public(err).Code != "LoginRateLimit" || retry <= 0 {
		t.Fatal("login budget not enforced")
	}
	first, _ := app.AcquireTransfer("one")
	second, _ := app.AcquireTransfer("one")
	if _, err := app.AcquireTransfer("one"); err == nil {
		t.Fatal("session limit ignored")
	}
	other, err := app.AcquireTransfer("two")
	if err != nil {
		t.Fatal(err)
	}
	first()
	second()
	other()
	if app.ActiveTransfers() != 0 {
		t.Fatal("leaked slots")
	}
}
func TestMutationScopeIsSharedAcrossSessionsAndServersAreIndependent(t *testing.T) {
	app := New(config.Default())
	one, _ := url.Parse("http://first:9000")
	two, _ := url.Parse("http://second:9000")
	a := &Session{id: "a", internal: one}
	b := &Session{id: "b", internal: one}
	c := &Session{id: "c", internal: two}
	release, _ := app.AcquireMutation(context.Background(), a, "iam")
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := app.AcquireMutation(ctx, b, "iam"); err != context.Canceled {
		t.Fatal(err)
	}
	for _, item := range []struct {
		s        *Session
		resource string
	}{{a, "bucket/a"}, {c, "iam"}} {
		done, err := app.AcquireMutation(context.Background(), item.s, item.resource)
		if err != nil {
			t.Fatal(err)
		}
		done()
	}
}

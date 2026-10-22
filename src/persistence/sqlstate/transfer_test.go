package sqlstate_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/secretbox"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
	"github.com/owiks/golang-minio-s3-backend-api/tests/databasefixtures"
)

func TestCrossDatabaseTransfer(t *testing.T) {
	if os.Getenv("STATE_INTEGRATION") != "1" {
		t.Skip("requires disposable SQL fixtures")
	}
	ctx := context.Background()
	ring, _ := secretbox.Generate()
	box, _ := secretbox.New(ring)
	cfg := databasefixtures.Database(t, "sqlite")
	source := authstate.NewManager(open(t, cfg), box, time.Hour)
	id, token := securetoken.New(), securetoken.New()
	if err := source.Bootstrap(ctx, authstate.Bootstrap{ID: id, ConnectionID: securetoken.New(), Endpoint: "https://s3.example.test", Region: "us-east-1", Mode: "bearer", Digest: authstate.Digest(token), ExpiresAt: time.Now().Add(time.Hour), Secrets: authstate.Secrets{AccessKey: "fixture", SecretKey: "private"}}); err != nil {
		t.Fatal(err)
	}
	initial, _, _ := source.Lookup(ctx, id)
	for _, driver := range []string{"postgres", "mysql", "sqlite"} {
		b, err := source.Export(ctx)
		if err != nil {
			t.Fatal(err)
		}
		cfg = databasefixtures.Database(t, driver)
		target := authstate.NewManager(open(t, cfg), box, time.Hour)
		if err = target.Restore(ctx, b, true); err != nil {
			t.Fatal(driver, err)
		}
		a, c, err := target.Lookup(ctx, id)
		if err != nil {
			t.Fatal(driver, err)
		}
		if ok, _ := a.Match(token, time.Now()); !ok || a.Snapshot().Revision != initial.Snapshot().Revision || !a.Snapshot().ExpiresAt.Equal(initial.Snapshot().ExpiresAt) {
			t.Fatal("transfer changed credential", driver)
		}
		if _, err = target.Decrypt(c); err != nil {
			t.Fatal(driver, err)
		}
		source = target
	}
	a, _, _ := source.Lookup(ctx, id)
	if err := source.Logout(ctx, authstate.CredentialGuard(a, false)); err != nil {
		t.Fatal(err)
	}
	b, err := source.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cfg = databasefixtures.Database(t, "postgres")
	target := authstate.NewManager(open(t, cfg), box, time.Hour)
	if err = target.Restore(ctx, b, true); err != nil {
		t.Fatal(err)
	}
	a, _, _ = target.Lookup(ctx, id)
	if a.Valid(time.Now()) {
		t.Fatal("transfer resurrected revoked credential")
	}
}

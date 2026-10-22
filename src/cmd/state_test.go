package cmd

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

func TestStateCommandsPreserveSecretsAndRevokeRestoredAccess(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	key := filepath.Join(dir, "keyring.json")
	backup := filepath.Join(dir, "state-backup.json")
	if err := stateCommand(ctx, []string{"keygen", "--out", key}); err != nil {
		t.Fatal(err)
	}
	if err := stateCommand(ctx, []string{"keygen", "--out", key}); err == nil {
		t.Fatal("keygen overwrote key")
	}
	info, err := os.Stat(key)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("key file not private", err)
	}
	t.Setenv("API_DB_DRIVER", "sqlite")
	t.Setenv("API_DB_SQLITE_PATH", filepath.Join(dir, "source.db"))
	t.Setenv("API_STATE_KEYRING_FILE", key)
	if err := migrateCommand(ctx, []string{"up"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	m, err := persistence.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	id, secret := securetoken.New(), securetoken.New()
	if err = m.Bootstrap(ctx, authstate.Bootstrap{ID: id, ConnectionID: securetoken.New(), Endpoint: "https://s3.example.test", Region: "us-east-1", Mode: "bearer", Digest: authstate.Digest(secret), ExpiresAt: time.Now().Add(time.Hour), Secrets: authstate.Secrets{AccessKey: "fixture", SecretKey: "fixture-private"}}); err != nil {
		t.Fatal(err)
	}
	_ = m.Close()
	for _, args := range [][]string{{"backup", "--out", backup}, {"key-add", "--file", key}, {"reencrypt"}, {"reencrypt"}, {"verify"}} {
		if err = stateCommand(ctx, args); err != nil {
			t.Fatal(args, err)
		}
	}
	t.Setenv("API_TARGET_DB_DRIVER", "sqlite")
	t.Setenv("API_TARGET_DB_SQLITE_PATH", filepath.Join(dir, "transferred.db"))
	if err := migrateCommand(ctx, []string{"up", "--target"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err = stateCommand(ctx, []string{"transfer", "--offline"}); err != nil {
		t.Fatal(err)
	}
	target := cfg
	target.State.SQLitePath = filepath.Join(dir, "transferred.db")
	m, err = persistence.Open(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	transferred, _, err := m.Lookup(ctx, id)
	_ = m.Close()
	if valid, _ := transferred.Match(secret, time.Now()); err != nil || !valid {
		t.Fatal("ENV-configured transfer changed active access", err)
	}
	// The old backup still decrypts using the retained old key, but restoration
	// cannot revive the previously issued access secret.
	t.Setenv("API_DB_SQLITE_PATH", filepath.Join(dir, "restored.db"))
	if err := migrateCommand(ctx, []string{"up"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err = stateCommand(ctx, []string{"restore", "--offline", "--in", backup}); err != nil {
		t.Fatal(err)
	}
	cfg, _ = config.Load()
	m, err = persistence.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	a, _, err := m.Lookup(ctx, id)
	if err != nil || a.Valid(time.Now()) {
		t.Fatal("restored credential revived", err)
	}
	if err = stateCommand(ctx, []string{"restore", "--offline", "--in", backup}); err == nil {
		t.Fatal("restore replaced populated database")
	}
}

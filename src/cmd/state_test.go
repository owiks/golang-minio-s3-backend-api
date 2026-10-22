package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/commandlog"
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
	id, secret, connection := securetoken.New(), securetoken.New(), securetoken.New()
	if err = m.Bootstrap(ctx, authstate.Bootstrap{ID: id, ConnectionID: connection, Endpoint: "https://s3.example.test", Region: "us-east-1", Mode: "bearer", Digest: authstate.Digest(secret), ExpiresAt: time.Now().Add(time.Hour), Secrets: authstate.Secrets{AccessKey: "fixture", SecretKey: "fixture-private"}}); err != nil {
		t.Fatal(err)
	}
	_ = m.Close()
	scope := commandlog.Scope{ActorID: id, ConnectionID: connection, Operation: "POST objects/transfer"}
	now := time.Now().UTC().Truncate(time.Microsecond)
	commands, err := persistence.OpenCommands(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	entry, _, err := commands.Reserve(ctx, scope, authstate.Digest("key"), authstate.Digest("body"), now)
	if err != nil {
		t.Fatal(err)
	}
	if err = commands.Finish(ctx, entry.ID, scope, &commandlog.Response{Status: 200}); err != nil {
		t.Fatal(err)
	}
	_ = commands.Close()
	for _, args := range [][]string{{"backup", "--out", backup}, {"key-add", "--file", key}, {"reencrypt"}, {"reencrypt"}, {"verify"}} {
		if err = stateCommand(ctx, args); err != nil {
			t.Fatal(args, err)
		}
	}
	raw, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	var saved persistence.Backup
	if json.Unmarshal(raw, &saved) != nil || len(saved.Commands) != 1 {
		t.Fatal("CLI backup omitted command fence")
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
	commands, err = persistence.OpenCommands(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	replay, fresh, err := commands.Reserve(ctx, scope, authstate.Digest("key"), authstate.Digest("body"), now)
	_ = commands.Close()
	if err != nil || fresh || replay.ID != entry.ID {
		t.Fatal("CLI transfer lost command fence", err)
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

type failedStateJSON struct{}

func (failedStateJSON) WriteJSON(w io.Writer) error {
	if _, err := io.WriteString(w, `{"partial":`); err != nil {
		return err
	}
	return errors.New("injected encoding failure")
}
func TestFailedStateEncodingPreservesExistingFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "keyring.json")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(target, failedStateJSON{}, true); err == nil {
		t.Fatal("partial state published")
	}
	raw, err := os.ReadFile(target)
	if err != nil || string(raw) != "original" {
		t.Fatal("encoding failure replaced original", err)
	}
	files, err := filepath.Glob(filepath.Join(dir, ".api-state-*"))
	if err != nil || len(files) != 0 {
		t.Fatal("private partial snapshot leaked", err)
	}
	var buf bytes.Buffer
	writer := &stateFileWriter{Writer: &buf, remaining: 4}
	if n, err := writer.Write([]byte("12345")); err == nil || n != 0 || buf.Len() != 0 {
		t.Fatal("state writer crossed byte budget")
	}
}

package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/database"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/migrations"
	"github.com/owiks/golang-minio-s3-backend-api/tests/migrationfixtures"
)

func TestMigrationCommandsDoNotNeedSecretsAndStatusDoesNotCreateSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	t.Setenv("API_DB_DRIVER", "sqlite")
	t.Setenv("API_DB_SQLITE_PATH", path)
	t.Setenv("API_STATE_KEYRING_FILE", "")
	t.Setenv("API_LISTEN", "invalid-for-HTTP")
	var output bytes.Buffer
	if err := migrateCommand(ctx, []string{"status"}, &output); err != nil || !strings.Contains(output.String(), `"current":0`) {
		t.Fatal("status", err)
	}
	var status migrations.Status
	if err := json.Unmarshal(output.Bytes(), &status); err != nil || status.Target < 1 || len(status.Entries) == 0 {
		t.Fatal("migration catalog", err)
	}
	output.Reset()
	if err := migrateCommand(ctx, []string{"version"}, &output); err != nil || output.String() != "0\n" {
		t.Fatal("version", err)
	}
	db, err := database.Open(ctx, database.Config{Database: config.Database{Driver: "sqlite", SQLitePath: path}})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE type='table'").Scan(&count) != nil || count != 0 {
		t.Fatal("status/version changed schema")
	}
	if err := migrateCommand(ctx, []string{"down"}, io.Discard); err == nil {
		t.Fatal("down without offline assertion accepted")
	}
	if err := migrateCommand(ctx, []string{"up"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := migrateCommand(ctx, []string{"version"}, &output); err != nil || output.String() != fmt.Sprintf("%d\n", status.Target) {
		t.Fatal("version after up", err)
	}
	if err := migrateCommand(ctx, []string{"down", "--offline"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	previous := int64(0)
	if len(status.Entries) > 1 {
		previous = status.Entries[len(status.Entries)-2].Version
	}
	if err := migrateCommand(ctx, []string{"version"}, &output); err != nil || output.String() != fmt.Sprintf("%d\n", previous) {
		t.Fatal("version after down", err)
	}
	for i := 1; i < len(status.Entries); i++ {
		if err := migrateCommand(ctx, []string{"down", "--offline"}, io.Discard); err != nil {
			t.Fatal("full rollback", err)
		}
	}
	output.Reset()
	if err := migrateCommand(ctx, []string{"version"}, &output); err != nil || output.String() != "0\n" {
		t.Fatal("version after full rollback", err)
	}
	if err := stateCommand(ctx, []string{"migrate"}); err != nil {
		t.Fatal("compatibility alias", err)
	}
}

func TestMemoryMigrationJobIsANoOp(t *testing.T) {
	t.Setenv("API_DB_DRIVER", "memory")
	for _, action := range []string{"up", "status", "version"} {
		if err := migrateCommand(context.Background(), []string{action}, io.Discard); err != nil {
			t.Fatal(action, err)
		}
	}
}

func TestLegacyMigrationRequiresAnExplicitOfflineAssertion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	t.Setenv("API_DB_DRIVER", "sqlite")
	t.Setenv("API_DB_SQLITE_PATH", path)
	db, err := database.Open(ctx, database.Config{Database: config.Database{Driver: "sqlite", SQLitePath: path}})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	raw, err := migrationfixtures.Files.ReadFile("legacy/sqlite/001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "INSERT INTO api_schema(version,checksum) VALUES(1,?)", fmt.Sprintf("%x", sha256.Sum256(raw))); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "INSERT INTO api_write_lock(id,revision) VALUES(1,37)"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"up"}, {"up", "--offline=false"}} {
		if err = migrateCommand(ctx, args, io.Discard); err == nil || !strings.Contains(err.Error(), "requires migrate up --offline") {
			t.Fatalf("legacy up %v = %v", args, err)
		}
	}
	if err = stateCommand(ctx, []string{"migrate"}); err == nil {
		t.Fatal("legacy compatibility alias accepted without offline assertion")
	}
	var count, revision int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE name='goose_db_version'").Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected migration changed Goose history", count, err)
	}
	if err = db.QueryRowContext(ctx, "SELECT revision FROM api_write_lock WHERE id=1").Scan(&revision); err != nil || revision != 37 {
		t.Fatal("rejected migration changed existing state", revision, err)
	}
	if err = migrateCommand(ctx, []string{"up", "--offline"}, io.Discard); err != nil {
		t.Fatal("explicit legacy adoption", err)
	}
}

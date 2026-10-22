package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/database"
	"github.com/owiks/golang-minio-s3-backend-api/tests/databasefixtures"
	"github.com/owiks/golang-minio-s3-backend-api/tests/migrationfixtures"
)

func eachDatabase(t *testing.T, run func(*testing.T, *Runner)) {
	t.Helper()
	drivers := []string{"sqlite"}
	if os.Getenv("STATE_INTEGRATION") == "1" {
		drivers = append(drivers, "postgres", "mysql")
	}
	for _, driver := range drivers {
		t.Run(driver, func(t *testing.T) {
			cfg := databasefixtures.Database(t, driver)
			db, err := database.Open(context.Background(), database.Config{Database: cfg.Database})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			r, err := New(db, driver, cfg.SQLitePath)
			if err != nil {
				t.Fatal(err)
			}
			run(t, r)
		})
	}
}

func TestVersionedMigrationLifecycle(t *testing.T) {
	eachDatabase(t, func(t *testing.T, r *Runner) {
		ctx := context.Background()
		initial, err := r.Status(ctx)
		if err != nil || initial.Target < 1 {
			t.Fatal("migration catalog", initial, err)
		}
		for i := 0; i < 2; i++ {
			s, err := r.Status(ctx)
			if err != nil || s.Current != 0 || s.Target != initial.Target {
				t.Fatal("empty status", s, err)
			}
			if !errors.Is(r.Check(ctx), ErrNotCurrent) {
				t.Fatal("empty schema accepted")
			}
		}
		if exists, err := tableExists(ctx, r.db, r.driver, goose.DefaultTablename); err != nil || exists {
			t.Fatal("read-only status/check created schema", err)
		}
		if err := r.Up(ctx); err != nil {
			t.Fatal(err)
		}
		if err := r.Up(ctx); err != nil {
			t.Fatal("repeat up", err)
		}
		if err := r.Check(ctx); err != nil {
			t.Fatal(err)
		}
		probeVersion := initial.Target + 1
		catalog := extend(t, r, fmt.Sprintf("%05d_probe.sql", probeVersion), "CREATE TABLE migration_probe (value TEXT NOT NULL);\nINSERT INTO migration_probe(value) VALUES('a;b');", "DROP TABLE migration_probe;")
		r.fsys = catalog
		if !errors.Is(r.Check(ctx), ErrNotCurrent) {
			t.Fatal("pending schema accepted")
		}
		if err := r.Up(ctx); err != nil {
			t.Fatal(err)
		}
		var value string
		if err := r.db.QueryRowContext(ctx, "SELECT value FROM migration_probe").Scan(&value); err != nil || value != "a;b" {
			t.Fatal("SQL parser damaged literal", err)
		}
		if err := r.Down(ctx); err != nil {
			t.Fatal(err)
		}
		if exists, _ := tableExists(ctx, r.db, r.driver, "migration_probe"); exists {
			t.Fatal("down did not run")
		}
		if err := r.Up(ctx); err != nil {
			t.Fatal("reapply", err)
		}
		failureVersion := probeVersion + 1
		failureName := fmt.Sprintf("%05d_failure.sql", failureVersion)
		r.fsys = extend(t, r, failureName, "CREATE TABLE migration_failure (value TEXT);\nINSERT INTO no_such_table(value) VALUES('private-error-detail');", "DROP TABLE migration_failure;")
		err = r.Up(ctx)
		var failed *Error
		if !errors.As(err, &failed) || failed.Version != failureVersion || failed.Name != failureName || strings.Contains(err.Error(), "private-error-detail") {
			t.Fatal("migration failure metadata unsafe or missing", err)
		}
		s, err := r.Status(ctx)
		if err != nil || s.Current != probeVersion || s.Entries[len(s.Entries)-1].Applied {
			t.Fatal("failed migration recorded as applied", s, err)
		}
		exists, _ := tableExists(ctx, r.db, r.driver, "migration_failure")
		if exists != (r.driver == "mysql") {
			t.Fatal("incorrect DDL transaction behaviour", r.driver, exists)
		}
		if !errors.Is(r.Check(ctx), ErrNotCurrent) {
			t.Fatal("failed migration schema accepted")
		}
		// A newer database must be rejected by the older compiled catalog.
		if !errors.Is(Check(ctx, r.db, r.driver), ErrNotCurrent) {
			t.Fatal("future database schema accepted")
		}
		exec(t, r, "DELETE FROM goose_db_version WHERE version_id=1")
		if !errors.Is(r.Down(ctx), ErrNotCurrent) {
			t.Fatal("rollback accepted inconsistent history")
		}
	})
}

func extend(t *testing.T, r *Runner, name, up, down string) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	entries, err := fs.ReadDir(r.fsys, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		raw, err := fs.ReadFile(r.fsys, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = &fstest.MapFile{Data: raw}
	}
	header := ""
	if r.driver == "mysql" {
		header = "-- +goose NO TRANSACTION\n"
	}
	out[name] = &fstest.MapFile{Data: []byte(header + "-- +goose Up\n" + up + "\n-- +goose Down\n" + down + "\n")}
	return out
}

func createLegacy(t *testing.T, r *Runner) {
	t.Helper()
	raw, err := migrationfixtures.Files.ReadFile("legacy/" + r.driver + "/001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != legacyHashes[r.driver] {
		t.Fatal("legacy fixture checksum changed")
	}
	header := "-- +goose Up\n"
	if r.driver == "mysql" {
		header = "-- +goose NO TRANSACTION\n" + header
	}
	p, err := r.provider(fstest.MapFS{"00001_legacy.sql": &fstest.MapFile{Data: append([]byte(header), raw...)}}, goose.WithDisableVersioning(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	exec(t, r, "INSERT INTO api_schema(version,checksum) VALUES(?,?)", 1, legacyHashes[r.driver])
	exec(t, r, "INSERT INTO api_write_lock(id,revision) VALUES(1,37)")
}

func exec(t *testing.T, r *Runner, query string, args ...any) {
	t.Helper()
	if r.driver == "postgres" {
		var n int
		for strings.Contains(query, "?") {
			n++
			query = strings.Replace(query, "?", fmt.Sprintf("$%d", n), 1)
		}
	}
	if _, err := r.db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func snapshot(t *testing.T, r *Runner) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, table := range []string{"api_connections", "api_accesses", "api_events", "api_schema", "api_write_lock"} {
		rows, err := r.db.Query("SELECT * FROM " + table + " ORDER BY 1")
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		out[table] = []string{}
		for rows.Next() {
			values := make([]sql.RawBytes, len(columns))
			dest := make([]any, len(columns))
			for i := range values {
				dest[i] = &values[i]
			}
			if rows.Scan(dest...) != nil {
				t.Fatal("snapshot scan")
			}
			for _, value := range values {
				out[table] = append(out[table], string(value))
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
	}
	return out
}

func TestLegacyAdoptionPreservesRows(t *testing.T) {
	eachDatabase(t, func(t *testing.T, r *Runner) {
		createLegacy(t, r)
		seedAuthentication(t, r)
		before := snapshot(t, r)
		if !errors.Is(r.Check(context.Background()), ErrNotCurrent) {
			t.Fatal("legacy accepted without explicit migration")
		}
		if err := r.Up(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := r.Check(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, snapshot(t, r)) {
			t.Fatal("legacy migration changed existing records")
		}
		if err := r.Up(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, snapshot(t, r)) {
			t.Fatal("repeated migration changed records")
		}
		verifyAuthentication(t, r)
	})
}

func TestLegacyMismatchDoesNotAdopt(t *testing.T) {
	eachDatabase(t, func(t *testing.T, r *Runner) {
		createLegacy(t, r)
		exec(t, r, "UPDATE api_schema SET checksum=?", "unexpected-checksum")
		before := snapshot(t, r)
		if !errors.Is(r.Up(context.Background()), ErrLegacy) {
			t.Fatal("unrecognised legacy schema adopted")
		}
		if !reflect.DeepEqual(before, snapshot(t, r)) {
			t.Fatal("refused migration changed application records")
		}
		if exists, _ := tableExists(context.Background(), r.db, r.driver, goose.DefaultTablename); exists {
			t.Fatal("refused migration registered Goose version")
		}
	})
}

func TestLegacyStructureMismatchDoesNotAdopt(t *testing.T) {
	eachDatabase(t, func(t *testing.T, r *Runner) {
		createLegacy(t, r)
		exec(t, r, "DROP TABLE api_events")
		if !errors.Is(r.Up(context.Background()), ErrLegacy) {
			t.Fatal("incomplete legacy schema adopted")
		}
		if exists, _ := tableExists(context.Background(), r.db, r.driver, goose.DefaultTablename); exists {
			t.Fatal("incomplete schema registered as applied")
		}
	})
}

func TestMigrationLockCancellationAndConcurrentUp(t *testing.T) {
	eachDatabase(t, func(t *testing.T, r *Runner) {
		release, err := r.acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		second, _ := New(r.db, r.driver, r.path)
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		err = second.Up(ctx)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			_ = release()
			t.Fatal("lock wait not cancelled", err)
		}
		if err = release(); err != nil {
			t.Fatal(err)
		}
		results := make(chan error, 2)
		go func() { results <- r.Up(context.Background()) }()
		go func() { results <- second.Up(context.Background()) }()
		for range 2 {
			if err = <-results; err != nil {
				t.Fatal("concurrent up", err)
			}
		}
		if err := r.Check(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

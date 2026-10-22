package persistence_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/secretbox"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/commandlog"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
	"github.com/owiks/golang-minio-s3-backend-api/tests/databasefixtures"
	"github.com/owiks/golang-minio-s3-backend-api/tests/statefixtures"
)

func keyfile(t *testing.T) string {
	t.Helper()
	ring, err := secretbox.Generate()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(ring)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "keys.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func database(t *testing.T, driver, key string) config.Config {
	t.Helper()
	state := databasefixtures.Database(t, driver)
	state.KeyringFile = key
	statefixtures.Migrate(t, state)
	return config.Config{State: state}
}
func manager(t *testing.T, cfg config.Config) *authstate.Manager {
	t.Helper()
	m, err := persistence.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}
func snapshot(t *testing.T, cfg config.Config, m *authstate.Manager) (persistence.Backup, string, string, commandlog.Scope, time.Time) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	id, connection, token := securetoken.New(), securetoken.New(), securetoken.New()
	if err := m.Bootstrap(ctx, authstate.Bootstrap{ID: id, ConnectionID: connection, Endpoint: "https://s3.example.test", Region: "us-east-1", Mode: "bearer", Digest: authstate.Digest(token), ExpiresAt: now.Add(time.Hour), Secrets: authstate.Secrets{AccessKey: "fixture", SecretKey: "private"}}); err != nil {
		t.Fatal(err)
	}
	commands, err := persistence.OpenCommands(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer commands.Close()
	scope := commandlog.Scope{ActorID: id, ConnectionID: connection, Operation: "POST objects/transfer"}
	entry, _, err := commands.Reserve(ctx, scope, authstate.Digest("done"), authstate.Digest("body"), now)
	if err != nil {
		t.Fatal(err)
	}
	if err = commands.Finish(ctx, entry.ID, scope, &commandlog.Response{Status: 200, Payload: json.RawMessage(`{"copied":true}`)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = commands.Reserve(ctx, scope, authstate.Digest("pending"), authstate.Digest("body"), now); err != nil {
		t.Fatal(err)
	}
	createOperations(t, cfg, operations.Scope{ActorID: id, ConnectionID: connection}, now)
	backup, err := persistence.ExportBackup(ctx, cfg, m)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(backup)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip persistence.Backup
	if err = json.Unmarshal(raw, &roundtrip); err != nil {
		t.Fatal(err)
	}
	return roundtrip, id, token, scope, now
}
func checkFences(t *testing.T, cfg config.Config, scope commandlog.Scope, now time.Time) {
	t.Helper()
	repo, err := persistence.OpenCommands(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	for _, key := range []string{"done", "pending"} {
		entry, fresh, err := repo.Reserve(context.Background(), scope, authstate.Digest(key), authstate.Digest("body"), now)
		if fresh {
			t.Fatal("transfer lost command fence", key)
		}
		if key == "done" && (err != nil || entry.State != commandlog.Completed) {
			t.Fatal("completed result not restored", err)
		}
		if key == "pending" && (!errors.Is(err, commandlog.ErrUnknown) || entry.State != commandlog.Unknown) {
			t.Fatal("orphaned pending owner can execute again", err)
		}
	}
}
func TestOfflineBackupTransferPreservesExecutionFences(t *testing.T) {
	key := keyfile(t)
	cfg := database(t, "sqlite", key)
	backup, id, token, scope, now := snapshot(t, cfg, manager(t, cfg))
	drivers := []string{"sqlite"}
	if os.Getenv("STATE_INTEGRATION") == "1" {
		drivers = append(drivers, "postgres", "mysql")
	}
	for _, driver := range drivers {
		t.Run(driver, func(t *testing.T) {
			target := database(t, driver, key)
			m := manager(t, target)
			if err := persistence.RestoreBackup(context.Background(), target, m, backup, true); err != nil {
				t.Fatal(err)
			}
			access, _, err := m.Lookup(context.Background(), id)
			valid, _ := access.Match(token, now)
			if err != nil || !valid {
				t.Fatal("transfer did not retain access", err)
			}
			checkFences(t, target, scope, now)
			checkOperations(t, target, scope, true, now)
		})
	}
	restored := database(t, "sqlite", key)
	m := manager(t, restored)
	if err := persistence.RestoreBackup(context.Background(), restored, m, backup, false); err != nil {
		t.Fatal(err)
	}
	a, _, err := m.Lookup(context.Background(), id)
	if err != nil || a.Valid(now) {
		t.Fatal("ordinary restore revived credential", err)
	}
	checkFences(t, restored, scope, now)
	checkOperations(t, restored, scope, false, now)
	// Legacy JSON remains readable for revoking restore, but cannot be used to
	// preserve access without demonstrating that command fences were captured.
	legacy := persistence.Backup{Backup: backup.Backup}
	legacyCfg := database(t, "sqlite", key)
	legacyManager := manager(t, legacyCfg)
	if err := persistence.RestoreBackup(context.Background(), legacyCfg, legacyManager, legacy, true); !errors.Is(err, commandlog.ErrInvalid) {
		t.Fatal("legacy backup retained unsafe access", err)
	}
	if err := persistence.RestoreBackup(context.Background(), legacyCfg, legacyManager, legacy, false); err != nil {
		t.Fatal("legacy backup rejected", err)
	}
	commandOnly := backup
	commandOnly.Operations = nil
	commandCfg := database(t, "sqlite", key)
	commandManager := manager(t, commandCfg)
	if err := persistence.RestoreBackup(context.Background(), commandCfg, commandManager, commandOnly, true); !errors.Is(err, operations.ErrInvalid) {
		t.Fatal("incomplete snapshot retained active access", err)
	}
	if err := persistence.RestoreBackup(context.Background(), commandCfg, commandManager, commandOnly, false); err != nil {
		t.Fatal("previous command snapshot restore rejected", err)
	}
}
func TestFailedOfflineTransferIsFailClosedAndResumable(t *testing.T) {
	ctx := context.Background()
	key := keyfile(t)
	source := database(t, "sqlite", key)
	backup, id, token, scope, now := snapshot(t, source, manager(t, source))
	target := database(t, "sqlite", keyfile(t))
	wrong := manager(t, target)
	if err := persistence.RestoreBackup(ctx, target, wrong, backup, true); err == nil {
		t.Fatal("wrong destination key accepted")
	}
	existing, err := wrong.Export(ctx)
	if err != nil || len(existing.Accesses)+len(existing.Connections) != 0 {
		t.Fatal("failed import published access", err)
	}
	checkFences(t, target, scope, now)
	checkOperations(t, target, scope, false, now)
	_ = wrong.Close()
	target.State.KeyringFile = key
	correct := manager(t, target)
	if err := persistence.RestoreBackup(ctx, target, correct, backup, true); err != nil {
		t.Fatal("offline rerun cannot resume imported fences", err)
	}
	a, _, err := correct.Lookup(ctx, id)
	valid, _ := a.Match(token, now)
	if err != nil || !valid {
		t.Fatal("resumed transfer failed", err)
	}
	checkFences(t, target, scope, now)
	checkOperations(t, target, scope, true, now)
}

func TestBackupJSONWritesCompactRecords(t *testing.T) {
	cfg := database(t, "sqlite", keyfile(t))
	backup, _, _, _, _ := snapshot(t, cfg, manager(t, cfg))
	// Pretty-printing this payload as a whole would multiply its nested padding.
	nested := strings.Repeat("[", 1000) + "0" + strings.Repeat("]", 1000)
	backup.Commands[0].State = commandlog.Completed
	backup.Commands[0].Response = &commandlog.Response{Status: 200, Payload: json.RawMessage(nested)}
	var buf bytes.Buffer
	if err := backup.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() > 16000 || !json.Valid(buf.Bytes()) {
		t.Fatal("snapshot expanded nested result or emitted invalid JSON", buf.Len())
	}
	var actual persistence.Backup
	if json.Unmarshal(buf.Bytes(), &actual) != nil || len(actual.Commands) != 2 || len(actual.Operations) != 2 {
		t.Fatal("snapshot fields lost")
	}
	if err := authstate.ValidateBackup(actual.Backup); err != nil {
		t.Fatal(err)
	}
}

func createOperations(t *testing.T, cfg config.Config, scope operations.Scope, now time.Time) {
	t.Helper()
	ctx := context.Background()
	jobs, err := persistence.OpenOperations(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer jobs.Close()
	if _, err = jobs.Insert(ctx, scope, operations.Spec{Scan: &operations.ScanSpec{Bucket: "source", Prefix: "prefix/"}}, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	scan, err := jobs.Claim(ctx, "source-worker", now, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = jobs.Save(ctx, scan.ID, scan.LeaseID, scan.Revision, operations.Checkpoint{Page: &operations.ScanPage{Objects: 3, Bytes: 12}}, now); err != nil {
		t.Fatal(err)
	}
	spec := operations.Spec{Transfers: []transfers.FileTransferRequest{{SourceBucket: "source", DestinationBucket: "target", SourceKey: "from", DestinationKey: "to", SourceRevision: "source-revision", DestinationRevision: "absent"}}}
	if _, err = jobs.Insert(ctx, scope, spec, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	transfer, err := jobs.Claim(ctx, "source-worker", now, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = jobs.Save(ctx, transfer.ID, transfer.LeaseID, transfer.Revision, operations.Checkpoint{Begin: true}, now); err != nil {
		t.Fatal(err)
	}
}
func checkOperations(t *testing.T, cfg config.Config, scope commandlog.Scope, recover bool, now time.Time) {
	t.Helper()
	ctx := context.Background()
	jobs, err := persistence.OpenOperations(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer jobs.Close()
	own := operations.Scope{ActorID: scope.ActorID, ConnectionID: scope.ConnectionID}
	records, err := jobs.List(ctx, own)
	if err != nil || len(records) != 2 {
		t.Fatal("operation records lost", len(records), err)
	}
	var interrupted operations.Snapshot
	for _, record := range records {
		if record.Kind == "scan" {
			if record.State != operations.Completed || record.Scan.Objects != 3 || record.Scan.Bytes != 12 {
				t.Fatal("scan checkpoint lost")
			}
		} else {
			interrupted = record
			if record.State != operations.Running || !record.InFlight {
				t.Fatal("in-flight fence lost")
			}
		}
	}
	if !recover {
		return
	}
	if _, err = jobs.Claim(ctx, "destination-worker", now.Add(time.Minute), 10*time.Second); !errors.Is(err, operations.ErrNotFound) {
		t.Fatal("transfer obtained second worker", err)
	}
	after, err := jobs.Get(ctx, own, interrupted.ID)
	if err != nil || after.State != operations.Unknown || len(after.Items) != 1 {
		t.Fatal("interrupted outcome was not fenced", err)
	}
}
func TestInvalidOperationSnapshotNeverPublishesAccessOrCommands(t *testing.T) {
	key := keyfile(t)
	cfg := database(t, "sqlite", key)
	backup, _, _, _, _ := snapshot(t, cfg, manager(t, cfg))
	backup.Operations[0].Revision = 0
	target := database(t, "sqlite", key)
	m := manager(t, target)
	if err := persistence.RestoreBackup(context.Background(), target, m, backup, true); !errors.Is(err, operations.ErrInvalid) {
		t.Fatal("invalid operation snapshot accepted", err)
	}
	access, err := m.Export(context.Background())
	if err != nil || len(access.Accesses) != 0 {
		t.Fatal("invalid batch published access", err)
	}
	commands, err := persistence.OpenCommands(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	defer commands.Close()
	entries, err := commands.Export(context.Background())
	if err != nil || len(entries) != 0 {
		t.Fatal("invalid batch published command fences", err)
	}
}

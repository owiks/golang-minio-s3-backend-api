package sqloperations_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/database"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/migrations"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/sqloperations"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
	"github.com/owiks/golang-minio-s3-backend-api/tests/databasefixtures"
	"github.com/owiks/golang-minio-s3-backend-api/tests/statefixtures"
)

func eachCooldownDatabase(t *testing.T, run func(*testing.T, config.State)) {
	t.Helper()
	drivers := []string{"sqlite"}
	if os.Getenv("STATE_INTEGRATION") == "1" {
		drivers = append(drivers, "postgres", "mysql")
	}
	for _, driver := range drivers {
		t.Run(driver, func(t *testing.T) {
			cfg := databasefixtures.Database(t, driver)
			statefixtures.Migrate(t, cfg)
			run(t, cfg)
		})
	}
}

func openCooldownStore(t *testing.T, cfg config.State) *sqloperations.Store {
	t.Helper()
	store, err := sqloperations.Open(context.Background(), sqloperations.Config{Database: cfg.Database})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func cooldownTransferSpec() operations.Spec {
	return operations.Spec{Transfers: []transfers.FileTransferRequest{{SourceBucket: "source", SourceKey: "key", SourceRevision: "checked", DestinationBucket: "target", DestinationKey: "key", DestinationRevision: "absent"}}}
}

func insertCooldownOperation(t *testing.T, store *sqloperations.Store, scope operations.Scope, spec operations.Spec, now time.Time) operations.Snapshot {
	t.Helper()
	record, err := store.Insert(context.Background(), scope, spec, now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func claimCooldownOperation(t *testing.T, store *sqloperations.Store, now time.Time) operations.Snapshot {
	t.Helper()
	record, err := store.Claim(context.Background(), "worker", now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func saveCooldownOperation(t *testing.T, store *sqloperations.Store, record operations.Snapshot, checkpoint operations.Checkpoint, now time.Time) operations.Snapshot {
	t.Helper()
	record, err := store.Save(context.Background(), record.ID, record.LeaseID, record.Revision, checkpoint, now)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestConnectionCooldownSurvivesRestartCancelAndCapacityEviction(t *testing.T) {
	eachCooldownDatabase(t, func(t *testing.T, cfg config.State) {
		ctx := context.Background()
		store := openCooldownStore(t, cfg)
		now := time.Now().UTC().Truncate(time.Microsecond)
		firstScope := operations.Scope{ActorID: "first", ConnectionID: "shared"}
		insertCooldownOperation(t, store, firstScope, operations.Spec{Scan: &operations.ScanSpec{Bucket: "bucket"}}, now)
		first := claimCooldownOperation(t, store, now)
		first = saveCooldownOperation(t, store, first, operations.Checkpoint{Throttle: true}, now)
		if first.State != operations.Queued || !first.NotBefore.After(now) || first.Scan.Pages != 0 || first.ThrottleTotal != 1 {
			t.Fatal("scan failure advanced progress or was not deferred")
		}
		secondScope := operations.Scope{ActorID: "second", ConnectionID: "shared"}
		second := insertCooldownOperation(t, store, secondScope, cooldownTransferSpec(), now.Add(time.Microsecond))
		other := insertCooldownOperation(t, store, operations.Scope{ActorID: "third", ConnectionID: "other"}, operations.Spec{Scan: &operations.ScanSpec{Bucket: "bucket"}}, now.Add(2*time.Microsecond))
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		store = openCooldownStore(t, cfg)
		until, err := store.Cooldown(ctx, "shared", now)
		if err != nil || !until.Equal(first.NotBefore) {
			t.Fatal("restart lost connection cooldown", until, err)
		}
		claimed := claimCooldownOperation(t, store, now.Add(3*time.Microsecond))
		if claimed.ID != other.ID {
			t.Fatal("paused connection blocked unrelated work")
		}
		_ = saveCooldownOperation(t, store, claimed, operations.Checkpoint{Page: &operations.ScanPage{}}, now.Add(3*time.Microsecond))
		if _, err = store.Claim(ctx, "worker", now.Add(4*time.Microsecond), time.Second); !errors.Is(err, operations.ErrNotFound) {
			t.Fatal("another actor or mutation bypassed cooldown", err)
		}
		first, err = store.Cancel(ctx, firstScope, first.ID, first.Revision, now.Add(5*time.Microsecond))
		if err != nil || first.State != operations.Cancelled || !first.NotBefore.Equal(until) {
			t.Fatal("cancel lost shared cooldown", err)
		}
		for i := range operations.MaxPerActor + 2 {
			stamp := now.Add(time.Duration(i+6) * time.Microsecond)
			v := insertCooldownOperation(t, store, firstScope, cooldownTransferSpec(), stamp)
			if _, err = store.Cancel(ctx, firstScope, v.ID, v.Revision, stamp); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = store.Get(ctx, firstScope, first.ID); err != nil {
			t.Fatal("capacity eviction removed active cooldown carrier", err)
		}
		if _, err = store.Claim(ctx, "worker", now.Add(time.Millisecond), time.Second); !errors.Is(err, operations.ErrNotFound) {
			t.Fatal("cancel or capacity eviction removed backpressure", err)
		}
		claimed = claimCooldownOperation(t, store, until)
		if claimed.ID != second.ID {
			t.Fatal("connection did not resume at exact cooldown deadline")
		}
		if until, err = store.Cooldown(ctx, "shared", until); err != nil || !until.IsZero() {
			t.Fatal("elapsed cooldown was not cleared", until, err)
		}
	})
}

func TestConnectionCooldownDoesNotHideExpiredMutationFence(t *testing.T) {
	eachCooldownDatabase(t, func(t *testing.T, cfg config.State) {
		ctx := context.Background()
		store := openCooldownStore(t, cfg)
		now := time.Now().UTC().Truncate(time.Microsecond)
		scanScope := operations.Scope{ActorID: "scan", ConnectionID: "shared"}
		insertCooldownOperation(t, store, scanScope, operations.Spec{Scan: &operations.ScanSpec{Bucket: "bucket"}}, now)
		scan := claimCooldownOperation(t, store, now)
		mutationScope := operations.Scope{ActorID: "mutation", ConnectionID: "shared"}
		insertCooldownOperation(t, store, mutationScope, cooldownTransferSpec(), now.Add(time.Microsecond))
		mutation := claimCooldownOperation(t, store, now.Add(time.Microsecond))
		mutation = saveCooldownOperation(t, store, mutation, operations.Checkpoint{Begin: true}, now.Add(time.Microsecond))
		insertCooldownOperation(t, store, operations.Scope{ActorID: "throttle", ConnectionID: "shared"}, operations.Spec{Scan: &operations.ScanSpec{Bucket: "bucket"}}, now.Add(2*time.Microsecond))
		throttled := claimCooldownOperation(t, store, now.Add(2*time.Microsecond))
		throttled = saveCooldownOperation(t, store, throttled, operations.Checkpoint{Throttle: true}, now.Add(2*time.Microsecond))
		if _, err := store.Claim(ctx, "recovery", now.Add(time.Second+time.Microsecond), time.Second); !errors.Is(err, operations.ErrNotFound) {
			t.Fatal("expired step executed while connection was paused", err)
		}
		recovered, err := store.Get(ctx, mutationScope, mutation.ID)
		if err != nil || recovered.State != operations.Unknown || recovered.InFlight || len(recovered.Items) != 1 {
			t.Fatal("cooldown hid uncertain mutation recovery", err)
		}
		waiting, err := store.Get(ctx, scanScope, scan.ID)
		if err != nil || waiting.State != operations.Queued || waiting.LeaseID != "" || !waiting.NotBefore.Equal(throttled.NotBefore) {
			t.Fatal("expired safe step did not yield behind cooldown", err)
		}
		if _, err = store.Save(ctx, scan.ID, scan.LeaseID, scan.Revision, operations.Checkpoint{Page: &operations.ScanPage{}}, now.Add(time.Second+time.Microsecond)); !errors.Is(err, operations.ErrConflict) {
			t.Fatal("stale scan lease accepted", err)
		}
		if _, err = store.Retry(ctx, mutationScope, recovered.ID, recovered.Revision, now.Add(time.Second)); !errors.Is(err, operations.ErrConflict) {
			t.Fatal("uncertain mutation became retryable", err)
		}
	})
}

func TestCooldownProjectionMigrationPreservesExistingOperation(t *testing.T) {
	eachCooldownDatabase(t, func(t *testing.T, cfg config.State) {
		ctx := context.Background()
		db, err := database.Open(ctx, database.Config{Database: cfg.Database})
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		runner, err := migrations.New(db, cfg.Driver, cfg.SQLitePath)
		if err != nil {
			t.Fatal(err)
		}
		if err = runner.Down(ctx); err != nil {
			t.Fatal(err)
		}
		status, err := runner.Status(ctx)
		if err != nil || status.Current != 3 {
			t.Fatal("fixture must represent schema before cooldown", status, err)
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		record, err := operations.New(operations.Scope{ActorID: "old", ConnectionID: "old"}, operations.Spec{Scan: &operations.ScanSpec{Bucket: "bucket"}}, now.Add(time.Hour), now)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		query := "INSERT INTO api_operations(id,actor_id,connection_id,state,revision,created_us,expires_us,lease_us,last_served_us,payload) VALUES(?,?,?,?,?,?,?,?,?,?)"
		if cfg.Driver == "postgres" {
			for i := 1; strings.Contains(query, "?"); i++ {
				query = strings.Replace(query, "?", "$"+strconv.Itoa(i), 1)
			}
		}
		if _, err = db.ExecContext(ctx, query, record.ID, record.Scope.ActorID, record.Scope.ConnectionID, record.State, record.Revision, now.UnixMicro(), 0, 0, 0, string(raw)); err != nil {
			t.Fatal(err)
		}
		if err = runner.Up(ctx); err != nil {
			t.Fatal(err)
		}
		var projected int64
		var preserved string
		if err = db.QueryRowContext(ctx, "SELECT not_before_us,payload FROM api_operations").Scan(&projected, &preserved); err != nil || projected != 0 || preserved != string(raw) {
			t.Fatal("migration changed existing operation", projected, err)
		}
		store := openCooldownStore(t, cfg)
		if got, err := store.Get(ctx, record.Scope, record.ID); err != nil || !operations.Same(got, record) {
			t.Fatal("pre-cooldown operation became unreadable", err)
		}
		if _, err = db.ExecContext(ctx, "UPDATE api_operations SET not_before_us=1"); err != nil {
			t.Fatal(err)
		}
		if _, err = store.Get(ctx, record.Scope, record.ID); !errors.Is(err, operations.ErrUnavailable) {
			t.Fatal("cooldown projection and payload mismatch accepted", err)
		}
	})
}

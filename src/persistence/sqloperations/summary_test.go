package sqloperations_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/sqloperations"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
	"github.com/owiks/golang-minio-s3-backend-api/tests/databasefixtures"
	"github.com/owiks/golang-minio-s3-backend-api/tests/statefixtures"
)

func TestOperatorSummaryAllDatabases(t *testing.T) {
	drivers := []string{"sqlite"}
	if os.Getenv("STATE_INTEGRATION") == "1" {
		drivers = append(drivers, "postgres", "mysql")
	}
	for _, driver := range drivers {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			cfg := databasefixtures.Database(t, driver)
			statefixtures.Migrate(t, cfg)
			store, err := sqloperations.Open(ctx, sqloperations.Config{Database: cfg.Database})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			now := time.Now().UTC().Truncate(time.Microsecond)
			first, err := store.Insert(ctx, operations.Scope{ActorID: "actor-a", ConnectionID: "private"}, operations.Spec{Scan: &operations.ScanSpec{Bucket: "private"}}, now.Add(time.Hour), now.Add(-10*time.Second))
			if err != nil {
				t.Fatal(err)
			}
			claimed, err := store.Claim(ctx, "worker", now.Add(-5*time.Second), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			_, err = store.Insert(ctx, operations.Scope{ActorID: "actor-b", ConnectionID: "private"}, operations.Spec{Scan: &operations.ScanSpec{Bucket: "private"}}, now.Add(time.Hour), now.Add(-3*time.Second))
			if err != nil {
				t.Fatal(err)
			}
			summary, err := store.Summary(ctx, now)
			if err != nil || summary.Scope != "store" || summary.Queued != 1 || summary.Running != 1 || summary.ExpiredLeases != 1 || summary.OldestQueuedAgeSec != 3 {
				t.Fatal(summary, err)
			}
			unchanged, err := store.Get(ctx, first.Scope, first.ID)
			if err != nil || unchanged.Revision != claimed.Revision || unchanged.State != operations.Running {
				t.Fatal("summary mutated execution fence", err)
			}
		})
	}
}

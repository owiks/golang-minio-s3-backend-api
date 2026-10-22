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

func TestDurableOperationTimingSurvivesRestartAllDatabases(t *testing.T) {
	drivers := []string{"sqlite"}
	if os.Getenv("STATE_INTEGRATION") == "1" {
		drivers = append(drivers, "postgres", "mysql")
	}
	for _, driver := range drivers {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			cfg := databasefixtures.Database(t, driver)
			statefixtures.Migrate(t, cfg)
			open := func() *sqloperations.Store {
				store, err := sqloperations.Open(ctx, sqloperations.Config{Database: cfg.Database})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = store.Close() })
				return store
			}
			r := open()
			now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			scope := operations.Scope{ActorID: "actor", ConnectionID: "connection"}
			s, err := r.Insert(ctx, scope, operations.Spec{Scan: &operations.ScanSpec{Bucket: "source"}}, now.Add(time.Hour), now)
			if err != nil {
				t.Fatal(err)
			}
			s, err = r.Claim(ctx, "worker", now.Add(2*time.Second), time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			s, err = r.Save(ctx, s.ID, s.LeaseID, s.Revision, operations.Checkpoint{Execute: true}, now.Add(3*time.Second))
			if err != nil {
				t.Fatal(err)
			}
			s, err = r.Save(ctx, s.ID, s.LeaseID, s.Revision, operations.Checkpoint{Page: &operations.ScanPage{Objects: 3, Bytes: 99, NextToken: "checkpoint"}}, now.Add(5*time.Second))
			if err != nil {
				t.Fatal(err)
			}
			if err = r.Close(); err != nil {
				t.Fatal(err)
			}
			r = open()
			s, err = r.Get(ctx, scope, s.ID)
			if err != nil {
				t.Fatal(err)
			}
			waiting, execution, known := s.Durations(now.Add(8 * time.Second))
			if !known || waiting != 6*time.Second || execution != 2*time.Second || s.Scan.Token != "checkpoint" {
				t.Fatal("checkpoint/timing lost after restart", waiting, execution, s.Timing)
			}
			s, err = r.Claim(ctx, "restarted", now.Add(8*time.Second), time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			s, err = r.Save(ctx, s.ID, s.LeaseID, s.Revision, operations.Checkpoint{Execute: true}, now.Add(9*time.Second))
			if err != nil {
				t.Fatal(err)
			}
			s, err = r.Save(ctx, s.ID, s.LeaseID, s.Revision, operations.Checkpoint{Page: &operations.ScanPage{Objects: 1, Bytes: 1}}, now.Add(12*time.Second))
			if err != nil {
				t.Fatal(err)
			}
			waiting, execution, known = s.Durations(now.Add(time.Hour))
			if !known || waiting != 7*time.Second || execution != 5*time.Second || s.State != operations.Completed || !s.Timing.StartedAt.Equal(now.Add(3*time.Second)) || !s.Timing.FinishedAt.Equal(now.Add(12*time.Second)) {
				t.Fatal("final cumulative timing", waiting, execution, s.State, s.Timing)
			}
			exported, err := r.Export(ctx)
			if err != nil || len(exported) != 1 || !operations.Same(s, exported[0]) {
				t.Fatal("timing backup changed", err)
			}
		})
	}
}

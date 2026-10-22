package sqloperations_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/sqloperations"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
	"github.com/owiks/golang-minio-s3-backend-api/tests/databasefixtures"
	"github.com/owiks/golang-minio-s3-backend-api/tests/statefixtures"
)

func TestDurableOperationsAllDatabases(t *testing.T) {
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
				v, err := sqloperations.Open(ctx, sqloperations.Config{Database: cfg.Database})
				if err != nil {
					t.Fatal(err)
				}
				return v
			}
			r := open()
			now := time.Now().UTC().Truncate(time.Microsecond)
			scope := operations.Scope{ActorID: "a", ConnectionID: "connection"}
			spec := operations.Spec{Transfers: []transfers.FileTransferRequest{{SourceBucket: "source", SourceKey: "key", SourceRevision: "checked", DestinationBucket: "target", DestinationKey: "key", DestinationRevision: "absent"}}}
			s, err := r.Insert(ctx, scope, spec, now.Add(time.Hour), now)
			if err != nil {
				t.Fatal(err)
			}
			if err = r.Close(); err != nil {
				t.Fatal(err)
			}
			r = open()
			defer r.Close()
			other := open()
			defer other.Close()
			got, err := r.Get(ctx, scope, s.ID)
			if err != nil || got.Spec.Transfers[0].SourceRevision != "checked" {
				t.Fatal(got, err)
			}
			if _, err = r.Get(ctx, operations.Scope{ActorID: "a", ConnectionID: "other"}, s.ID); !errors.Is(err, operations.ErrNotFound) {
				t.Fatal("scope leak", err)
			}
			var wg sync.WaitGroup
			claims := make(chan operations.Snapshot, 2)
			errs := make(chan error, 2)
			for _, store := range []*sqloperations.Store{r, other} {
				wg.Add(1)
				go func(store *sqloperations.Store) {
					defer wg.Done()
					v, claimErr := store.Claim(ctx, "worker", now, time.Minute)
					if claimErr != nil {
						errs <- claimErr
					} else {
						claims <- v
					}
				}(store)
			}
			wg.Wait()
			close(claims)
			close(errs)
			if len(claims) != 1 || len(errs) != 1 {
				t.Fatal("claim not exclusive", len(claims), len(errs))
			}
			for err := range errs {
				if !errors.Is(err, operations.ErrNotFound) {
					t.Fatal(err)
				}
			}
			claimed := <-claims
			started, err := r.Save(ctx, s.ID, claimed.LeaseID, claimed.Revision, operations.Checkpoint{Begin: true}, now)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = other.Claim(ctx, "restarted", now.Add(time.Minute), time.Minute); !errors.Is(err, operations.ErrNotFound) {
				t.Fatal("mutation rerun", err)
			}
			got, err = r.Get(ctx, scope, s.ID)
			if err != nil || got.State != operations.Unknown {
				t.Fatal(got, err)
			}
			unchanged, err := r.Cancel(ctx, scope, s.ID, got.Revision, now.Add(time.Minute))
			if err != nil || unchanged.Revision != got.Revision {
				t.Fatal("terminal cancel no-op", unchanged, err)
			}
			if _, err = r.Save(ctx, s.ID, started.LeaseID, started.Revision, operations.Checkpoint{Transfer: &operations.TransferOutcome{Copied: true}}, now.Add(time.Minute)); !errors.Is(err, operations.ErrConflict) {
				t.Fatal("stale lease", err)
			}
			if err = r.Prune(ctx, got.ExpiresAt); err != nil {
				t.Fatal(err)
			}
			if _, err = r.Get(ctx, scope, s.ID); !errors.Is(err, operations.ErrNotFound) {
				t.Fatal("retention", err)
			}
			scan, err := r.Insert(ctx, scope, operations.Spec{Scan: &operations.ScanSpec{Bucket: "source"}}, now.Add(time.Hour), now)
			if err != nil {
				t.Fatal(err)
			}
			claimed, err = r.Claim(ctx, "scan", now, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			scan, err = r.Save(ctx, scan.ID, claimed.LeaseID, claimed.Revision, operations.Checkpoint{Page: &operations.ScanPage{Objects: 3, Bytes: 99, NextToken: "next"}}, now)
			if err != nil {
				t.Fatal(err)
			}
			if err = r.Close(); err != nil {
				t.Fatal(err)
			}
			r = open()
			defer r.Close()
			claimed, err = r.Claim(ctx, "scan", now.Add(time.Second), time.Minute)
			if err != nil || claimed.Scan.Token != "next" || claimed.Scan.Objects != 3 {
				t.Fatal(claimed, err)
			}
			scan, err = r.Save(ctx, scan.ID, claimed.LeaseID, claimed.Revision, operations.Checkpoint{Page: &operations.ScanPage{Objects: 1, Bytes: 1}}, now.Add(time.Second))
			if err != nil || scan.State != operations.Completed || scan.Scan.Objects != 4 || scan.Scan.Bytes != 100 {
				t.Fatal(scan, err)
			}
			if _, err = r.Cancel(ctx, scope, scan.ID, scan.Revision, now.Add(time.Second)); err != nil {
				t.Fatal("completed cancel no-op", err)
			}
			exported, err := r.Export(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err = r.Import(ctx, exported); err != nil {
					t.Fatal("repeat exact backup", err)
				}
			}
			exported[0].Spec.Scan.Prefix = "changed"
			if err = r.Import(ctx, exported); !errors.Is(err, operations.ErrConflict) {
				t.Fatal("backup overwrite", err)
			}
			for i := 0; i < operations.MaxPerActor+2; i++ {
				v, insertErr := r.Insert(ctx, scope, spec, now.Add(time.Hour), now.Add(time.Duration(i+2)*time.Microsecond))
				if insertErr != nil {
					t.Fatal("daily operation quota", i, insertErr)
				}
				v, cancelErr := r.Cancel(ctx, scope, v.ID, v.Revision, now.Add(time.Duration(i+2)*time.Microsecond))
				if cancelErr != nil {
					t.Fatal(cancelErr)
				}
				if _, cancelErr = r.Cancel(ctx, scope, v.ID, v.Revision, now.Add(time.Duration(i+2)*time.Microsecond)); cancelErr != nil {
					t.Fatal("repeated queued cancel", cancelErr)
				}
			}
		})
	}
}

package persistence_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/database"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/migrations"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/sqlstate"
	"github.com/owiks/golang-minio-s3-backend-api/tests/databasefixtures"
)

func TestRepositoryOpenDoesNotMigrate(t *testing.T) {
	drivers := []string{"sqlite"}
	if os.Getenv("STATE_INTEGRATION") == "1" {
		drivers = append(drivers, "postgres", "mysql")
	}
	for _, driver := range drivers {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			cfg := databasefixtures.Database(t, driver)
			if s, err := sqlstate.Open(ctx, sqlstate.Config{Database: cfg.Database}); !errors.Is(err, migrations.ErrNotCurrent) {
				if s != nil {
					_ = s.Close()
				}
				t.Fatal("unmigrated repository opened", err)
			}
			db, err := database.Open(ctx, database.Config{Database: cfg.Database})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			for _, table := range []string{"goose_db_version", "api_connections", "api_accesses", "api_events"} {
				if rows, err := db.QueryContext(ctx, "SELECT * FROM "+table); err == nil {
					_ = rows.Close()
					t.Fatal("repository created table", table)
				}
			}
		})
	}
}

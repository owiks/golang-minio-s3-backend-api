package sqlstate_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/sqlstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/tests/databasefixtures"
	"github.com/owiks/golang-minio-s3-backend-api/tests/statecontract"
	"github.com/owiks/golang-minio-s3-backend-api/tests/statefixtures"
)

func TestSQLiteContract(t *testing.T) {
	s := open(t, databasefixtures.Database(t, "sqlite"))
	statecontract.Run(t, s)
}
func TestSQLNativeContract(t *testing.T) {
	if os.Getenv("STATE_INTEGRATION") != "1" {
		t.Skip("requires disposable PostgreSQL/MySQL fixtures")
	}
	for _, driver := range []string{"postgres", "mysql"} {
		t.Run(driver, func(t *testing.T) {
			cfg := databasefixtures.Database(t, driver)
			statecontract.Run(t, open(t, cfg))
		})
	}
}

func open(t *testing.T, cfg config.State) *sqlstate.Store {
	t.Helper()
	statefixtures.Migrate(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := sqlstate.Open(ctx, sqlstate.Config{Database: cfg.Database})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSQLClockContract(t *testing.T) {
	drivers := []string{"sqlite"}
	if os.Getenv("STATE_INTEGRATION") == "1" {
		drivers = append(drivers, "postgres", "mysql")
	}
	for _, driver := range drivers {
		t.Run(driver, func(t *testing.T) {
			statecontract.RunClock(t, func(t *testing.T, now func() time.Time) authstate.Repository {
				cfg := databasefixtures.Database(t, driver)
				statefixtures.Migrate(t, cfg)
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				s, err := sqlstate.OpenWithClock(ctx, sqlstate.Config{Database: cfg.Database}, now)
				if err != nil {
					t.Fatal(err)
				}
				return s
			})
		})
	}
}

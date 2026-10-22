package statefixtures

import (
	"context"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/database"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/migrations"
)

func Migrate(t *testing.T, state config.State) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.Open(ctx, database.Config{Database: state.Database})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r, err := migrations.New(db, state.Driver, state.SQLitePath)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Up(ctx); err != nil {
		t.Fatal(err)
	}
}

package persistence

import (
	"context"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/database"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/sqloperations"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
)

func OpenOperations(ctx context.Context, c config.Config) (operations.Repository, error) {
	if c.State.Driver == "memory" || c.State.Driver == "" {
		return operations.NewMemory(), nil
	}
	return sqloperations.Open(ctx, database.Config{Database: c.State.Database, MaxOpen: c.State.MaxOpen, MaxIdle: c.State.MaxIdle, Lifetime: c.State.Lifetime})
}

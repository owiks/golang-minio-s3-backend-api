package persistence

import (
	"context"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/database"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/sqlcommandlog"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/commandlog"
)

func OpenCommands(ctx context.Context, c config.Config) (commandlog.Repository, error) {
	if c.State.Driver == "memory" || c.State.Driver == "" {
		return commandlog.NewMemory(), nil
	}
	return sqlcommandlog.Open(ctx, database.Config{Database: c.State.Database, MaxOpen: c.State.MaxOpen, MaxIdle: c.State.MaxIdle, Lifetime: c.State.Lifetime})
}

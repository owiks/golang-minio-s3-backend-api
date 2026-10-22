// Package persistence composes durable access adapters at process startup.
package persistence

import (
	"context"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/secretbox"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/sqlstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
)

func Open(ctx context.Context, c config.Config) (*authstate.Manager, error) {
	var box *secretbox.Box
	var err error
	if c.State.Driver == "memory" || c.State.Driver == "" {
		box, err = secretbox.Ephemeral()
	} else {
		box, err = secretbox.Load(c.State.KeyringFile)
	}
	if err != nil {
		return nil, err
	}
	var repo authstate.Repository
	if c.State.Driver == "memory" || c.State.Driver == "" {
		repo = authstate.NewMemory()
	} else {
		repo, err = sqlstate.Open(ctx, sqlstate.Config{Database: c.State.Database, MaxOpen: c.State.MaxOpen, MaxIdle: c.State.MaxIdle, Lifetime: c.State.Lifetime})
		if err != nil {
			return nil, err
		}
	}
	m := authstate.NewManager(repo, box, c.WithDefaults().TokenTTL)
	check, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err = m.Verify(check); err != nil {
		_ = repo.Close()
		return nil, err
	}
	return m, nil
}

// Package database owns SQL connections. Opening a connection never runs schema migrations.
package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
)

var ErrOpen = errors.New("cannot connect to API state database")

type Config struct {
	config.Database
	MaxOpen  int
	MaxIdle  int
	Lifetime time.Duration
}

func Open(ctx context.Context, c Config) (*sql.DB, error) {
	db, err := connect(c)
	if err != nil {
		return nil, ErrOpen
	}
	maxOpen, maxIdle := c.MaxOpen, c.MaxIdle
	if maxOpen < 1 {
		maxOpen = 10
	}
	if maxIdle < 1 {
		maxIdle = 2
	}
	if c.Driver == "sqlite" {
		maxOpen, maxIdle = 1, 1
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(min(maxIdle, maxOpen))
	lifetime := c.Lifetime
	if lifetime <= 0 {
		lifetime = 30 * time.Minute
	}
	db.SetConnMaxLifetime(lifetime)
	if db.PingContext(ctx) != nil {
		_ = db.Close()
		return nil, ErrOpen
	}
	if c.Driver == "sqlite" {
		if _, err = db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
			_ = db.Close()
			return nil, ErrOpen
		}
	}
	return db, nil
}

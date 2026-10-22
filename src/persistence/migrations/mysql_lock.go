package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// MySQL advisory locks belong to the physical SQL session. Keep one connection
// until RELEASE_LOCK; discard it if lock ownership or cleanup is uncertain.
func (r *Runner) acquireMySQL(ctx context.Context) (func() error, error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	discard := func() { _ = conn.Raw(func(any) error { return driver.ErrBadConn }); _ = conn.Close() }
	var name string
	if err = conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&name); err != nil {
		discard()
		return nil, err
	}
	hash := sha256.Sum256([]byte(strings.ToLower(name)))
	key := "minio-api-schema-" + hex.EncodeToString(hash[:16])
	var acquired sql.NullInt64
	err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,30)", key).Scan(&acquired)
	if err != nil || !acquired.Valid || acquired.Int64 != 1 {
		discard()
		if err == nil {
			err = errors.New("migration lock acquisition timed out")
		}
		return nil, err
	}
	return func() error {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var released sql.NullInt64
		if err := conn.QueryRowContext(cleanup, "SELECT RELEASE_LOCK(?)", key).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
			discard()
			return failure("unlock", nil)
		}
		if conn.Close() != nil {
			return failure("unlock", nil)
		}
		return nil
	}, nil
}

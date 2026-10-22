package migrations

import (
	"context"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/pressly/goose/v3/lock"
)

// The lock spans legacy validation, baseline registration and Goose execution.
func (r *Runner) locked(ctx context.Context, run func() error) (err error) {
	release, err := r.acquire(ctx)
	if err != nil {
		return failure("lock", err)
	}
	defer func() { err = errors.Join(err, release()) }()
	return run()
}

func (r *Runner) acquire(ctx context.Context) (func() error, error) {
	if r.driver == "sqlite" {
		path, err := filepath.EvalSymlinks(r.path)
		if err != nil {
			return nil, err
		}
		path, err = filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		f := flock.New(path + ".goose.lock")
		ok, err := f.TryLockContext(ctx, 50*time.Millisecond)
		if err != nil || !ok {
			_ = f.Close()
			if err == nil {
				err = ctx.Err()
				if err == nil {
					err = errors.New("migration lock not acquired")
				}
			}
			return nil, err
		}
		return func() error {
			if f.Close() != nil {
				return failure("unlock", nil)
			}
			return nil
		}, nil
	}
	if r.driver == "mysql" {
		return r.acquireMySQL(ctx)
	}
	l, err := lock.NewPostgresSessionLocker(lock.WithLockID(714920351), lock.WithLockTimeout(1, 30), lock.WithUnlockTimeout(1, 3))
	if err != nil {
		return nil, err
	}
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	if err = l.SessionLock(ctx, conn); err != nil {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = conn.Close()
		return nil, err
	}
	return func() error {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if l.SessionUnlock(cleanup, conn) != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			_ = conn.Close()
			return failure("unlock", nil)
		}
		if conn.Close() != nil {
			return failure("unlock", nil)
		}
		return nil
	}, nil
}

// Package migrations owns explicit Goose schema changes, independently of repositories.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
)

//go:embed sql/*/*.sql
var files embed.FS

var (
	ErrNotCurrent = errors.New("API database schema is not current; run api migrate up")
	ErrLegacy     = errors.New("legacy API schema does not match the supported initial schema")
	ErrUnmanaged  = errors.New("unversioned or incomplete API schema; inspect it before migrating")
)

type Runner struct {
	db      *sql.DB
	driver  string
	path    string
	fsys    fs.FS
	dialect goose.Dialect
}

type Entry struct {
	Version int64  `json:"version"`
	Name    string `json:"name"`
	Applied bool   `json:"applied"`
}

type Status struct {
	Current int64   `json:"current"`
	Target  int64   `json:"target"`
	Legacy  bool    `json:"legacy"`
	Entries []Entry `json:"entries"`
}

func New(db *sql.DB, driver, sqlitePath string) (*Runner, error) {
	dialect := map[string]goose.Dialect{"postgres": goose.DialectPostgres, "mysql": goose.DialectMySQL, "sqlite": goose.DialectSQLite3}[driver]
	if dialect == "" {
		return nil, ErrUnmanaged
	}
	source, err := fs.Sub(files, "sql/"+driver)
	if err != nil {
		return nil, ErrUnmanaged
	}
	return &Runner{db: db, driver: driver, path: sqlitePath, fsys: source, dialect: dialect}, nil
}

func (r *Runner) provider(source fs.FS, options ...goose.ProviderOption) (*goose.Provider, error) {
	base := []goose.ProviderOption{goose.WithDisableGlobalRegistry(true), goose.WithLogger(goose.NopLogger())}
	return goose.NewProvider(r.dialect, r.db, source, append(base, options...)...)
}

// Status does not use Provider.Status: Goose initialises its version table there.
// This path is also used by API startup, so it must only read existing metadata.
func (r *Runner) Status(ctx context.Context) (Status, error) {
	p, err := r.provider(r.fsys)
	if err != nil {
		return Status{}, failure("status", err)
	}
	status := Status{Entries: []Entry{}}
	applied := map[int64]bool{}
	exists, err := tableExists(ctx, r.db, r.driver, goose.DefaultTablename)
	if err != nil {
		return status, failure("status", err)
	}
	if exists {
		store, err := database.NewStore(r.dialect, goose.DefaultTablename)
		if err != nil {
			return status, failure("status", err)
		}
		rows, err := store.ListMigrations(ctx, r.db)
		if err != nil {
			return status, failure("status", err)
		}
		for _, row := range rows {
			if !row.IsApplied {
				continue
			}
			if applied[row.Version] || row.Version < 0 {
				return status, ErrNotCurrent
			}
			applied[row.Version] = true
			status.Current = max(status.Current, row.Version)
		}
	}
	known := map[int64]bool{0: true}
	for _, s := range p.ListSources() {
		known[s.Version] = true
		status.Target = max(status.Target, s.Version)
		status.Entries = append(status.Entries, Entry{Version: s.Version, Name: filepath.Base(s.Path), Applied: applied[s.Version]})
	}
	for version := range applied {
		if !known[version] {
			return status, ErrNotCurrent
		}
	}
	for _, entry := range status.Entries {
		if entry.Version <= status.Current && !entry.Applied {
			return status, ErrNotCurrent
		}
	}
	if status.Current == 0 {
		status.Legacy, err = tableExists(ctx, r.db, r.driver, "api_schema")
		if err != nil {
			return status, failure("status", err)
		}
	}
	return status, nil
}

func Check(ctx context.Context, db *sql.DB, driver string) error {
	r, err := New(db, driver, "")
	if err != nil {
		return err
	}
	return r.Check(ctx)
}

func (r *Runner) Check(ctx context.Context) error {
	status, err := r.Status(ctx)
	if err != nil {
		return err
	}
	if status.Current != status.Target || status.Legacy {
		return ErrNotCurrent
	}
	for _, e := range status.Entries {
		if !e.Applied {
			return ErrNotCurrent
		}
	}
	return validateTables(ctx, r.db)
}

func (r *Runner) Up(ctx context.Context) error {
	return r.locked(ctx, func() error {
		status, err := r.Status(ctx)
		if err != nil {
			return err
		}
		if status.Current == 0 {
			if status.Legacy {
				if err = r.validateLegacy(ctx); err != nil {
					return err
				}
				// Record the existing version through Goose itself, without replaying DDL.
				baseline, err := r.provider(nil, goose.WithGoMigrations(goose.NewGoMigration(1, nil, nil)))
				if err != nil {
					return failure("baseline", err)
				}
				if _, err = baseline.Up(ctx); err != nil {
					return failure("baseline", err)
				}
			} else {
				for _, name := range []string{"api_connections", "api_accesses", "api_events", "api_write_lock"} {
					present, err := tableExists(ctx, r.db, r.driver, name)
					if err != nil {
						return failure("up", err)
					}
					if present {
						return ErrUnmanaged
					}
				}
			}
		}
		p, err := r.provider(r.fsys)
		if err != nil {
			return failure("up", err)
		}
		if _, err = p.Up(ctx); err != nil {
			return failure("up", err)
		}
		return nil
	})
}

func (r *Runner) Down(ctx context.Context) error {
	return r.locked(ctx, func() error {
		status, err := r.Status(ctx)
		if err != nil {
			return err
		}
		if status.Legacy {
			return ErrNotCurrent
		}
		if status.Current == 0 {
			return nil
		}
		p, err := r.provider(r.fsys)
		if err != nil {
			return failure("down", err)
		}
		if _, err = p.Down(ctx); err != nil {
			return failure("down", err)
		}
		return nil
	})
}

type Error struct {
	Operation string
	Version   int64
	Name      string
}

func (e *Error) Error() string {
	if e.Version != 0 {
		return fmt.Sprintf("migration %s failed: version=%d file=%s; inspect migration status", e.Operation, e.Version, e.Name)
	}
	return "migration " + e.Operation + " failed; check database availability and migration status"
}
func failure(operation string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("migration %s: %w", operation, err)
	}
	e := &Error{Operation: operation}
	var partial *goose.PartialError
	if errors.As(err, &partial) && partial.Failed != nil && partial.Failed.Source != nil {
		e.Version, e.Name = partial.Failed.Source.Version, filepath.Base(partial.Failed.Source.Path)
	}
	return e
}

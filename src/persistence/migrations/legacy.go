package migrations

import (
	"context"
	"database/sql"
)

// Hashes of the exact 001_initial.sql files shipped by the previous runner.
var legacyHashes = map[string]string{
	"postgres": "874cd1ff3cc5d0479f9d09c8f5ead01bf31f33ee8cbaa570a0b0582049ce241e",
	"mysql":    "f8d0a4339649aab7fc672e3ad65e72e409c0544e421bf668ecd751ccb3b46ee7",
	"sqlite":   "b3ab2e624ec3b0a7222ac4829118fd6aa0d6cff9b3cab1092083b8143b0efdbe",
}

func (r *Runner) validateLegacy(ctx context.Context) error {
	rows, err := r.db.QueryContext(ctx, "SELECT version,checksum FROM api_schema")
	if err != nil {
		return ErrLegacy
	}
	var version int
	var checksum string
	valid := rows.Next() && rows.Scan(&version, &checksum) == nil && version == 1 && checksum == legacyHashes[r.driver]
	valid = valid && !rows.Next()
	err, closeErr := rows.Err(), rows.Close()
	if !valid || err != nil || closeErr != nil {
		return ErrLegacy
	}
	if validateTables(ctx, r.db) != nil {
		return ErrLegacy
	}
	return nil
}

func validateTables(ctx context.Context, db *sql.DB) error {
	for _, query := range []string{
		"SELECT id,owner,payload FROM api_connections WHERE 1=0",
		"SELECT id,owner,connection_id,payload FROM api_accesses WHERE 1=0",
		"SELECT id,owner,at_us,payload FROM api_events WHERE 1=0",
	} {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return ErrNotCurrent
		}
		err, closeErr := rows.Err(), rows.Close()
		if err != nil || closeErr != nil {
			return ErrNotCurrent
		}
	}
	rows, err := db.QueryContext(ctx, "SELECT id,revision FROM api_write_lock")
	if err != nil {
		return ErrNotCurrent
	}
	var id, revision int64
	valid := rows.Next() && rows.Scan(&id, &revision) == nil && id == 1 && revision >= 0
	valid = valid && !rows.Next()
	err, closeErr := rows.Err(), rows.Close()
	if !valid || err != nil || closeErr != nil {
		return ErrNotCurrent
	}
	return nil
}

func tableExists(ctx context.Context, db *sql.DB, driver, name string) (bool, error) {
	query := "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?"
	switch driver {
	case "postgres":
		query = "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=$1"
	case "sqlite":
		query = "SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name=?"
	}
	var count int
	err := db.QueryRowContext(ctx, query, name).Scan(&count)
	return count == 1, err
}

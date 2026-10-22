package sqlcommandlog

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/commandlog"
)

func (s *Store) entries(ctx context.Context, tx *sql.Tx) ([]commandlog.Entry, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id,actor_id,connection_id,operation,key_hash,expires_us,payload FROM api_commands ORDER BY id")
	if err != nil {
		return nil, commandlog.ErrUnavailable
	}
	defer rows.Close()
	entries := []commandlog.Entry{}
	total := 0
	for rows.Next() {
		entry, err := readEntry(rows)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			return nil, commandlog.ErrUnavailable
		}
		total += len(raw)
		if total > commandlog.MaxSnapshotBytes {
			return nil, commandlog.ErrLimit
		}
		entries = append(entries, entry)
		if len(entries) > commandlog.MaxRecords {
			return nil, commandlog.ErrUnavailable
		}
	}
	if rows.Err() != nil {
		return nil, commandlog.ErrUnavailable
	}
	return entries, nil
}

func (s *Store) Export(ctx context.Context) ([]commandlog.Entry, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	entries, err := s.entries(ctx, tx)
	if err != nil {
		return nil, err
	}
	if tx.Commit() != nil {
		return nil, commandlog.ErrUnavailable
	}
	return entries, nil
}

func (s *Store) Import(ctx context.Context, entries []commandlog.Entry) error {
	if err := commandlog.ValidateEntries(entries); err != nil {
		return err
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	existing, err := s.entries(ctx, tx)
	if err != nil {
		return err
	}
	byID := make(map[string]commandlog.Entry, len(entries))
	for _, entry := range entries {
		byID[entry.ID] = entry
	}
	present := make(map[string]bool, len(existing))
	for _, entry := range existing {
		next, ok := byID[entry.ID]
		if !ok || !commandlog.SameEntry(entry, next) {
			return commandlog.ErrConflict
		}
		present[entry.ID] = true
	}
	for _, entry := range entries {
		if present[entry.ID] {
			continue
		}
		payload, err := json.Marshal(entry)
		if err != nil || len(payload) > commandlog.MaxEntryBytes {
			return commandlog.ErrInvalid
		}
		if _, err = tx.ExecContext(ctx, s.q("INSERT INTO api_commands(id,actor_id,connection_id,operation,key_hash,expires_us,payload) VALUES(?,?,?,?,?,?,?)"), entry.ID, entry.Scope.ActorID, entry.Scope.ConnectionID, entry.Scope.Operation, entry.KeyHash, entry.ExpiresAt.UnixMicro(), string(payload)); err != nil {
			return commandlog.ErrUnavailable
		}
	}
	if tx.Commit() != nil {
		return commandlog.ErrUnavailable
	}
	return nil
}

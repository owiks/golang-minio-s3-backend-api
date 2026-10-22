// Package sqlcommandlog persists command execution fences independently of auth
// mutations. The lock row serializes reservations in every supported SQL dialect.
package sqlcommandlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/database"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/migrations"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/commandlog"
)

type Config = database.Config

type Store struct {
	db     *sql.DB
	driver string
}

func Open(ctx context.Context, cfg Config) (*Store, error) {
	db, err := database.Open(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err = migrations.Check(ctx, db, cfg.Driver); err != nil {
		_ = db.Close()
		return nil, err
	}
	rows, err := db.QueryContext(ctx, "SELECT id,actor_id,connection_id,operation,key_hash,expires_us,payload FROM api_commands WHERE 1=0")
	if err != nil {
		_ = db.Close()
		return nil, commandlog.ErrUnavailable
	}
	if rows.Close() != nil {
		_ = db.Close()
		return nil, commandlog.ErrUnavailable
	}
	var id int
	if db.QueryRowContext(ctx, "SELECT id FROM api_command_lock WHERE id=1").Scan(&id) != nil || id != 1 {
		_ = db.Close()
		return nil, commandlog.ErrUnavailable
	}
	return &Store{db: db, driver: cfg.Driver}, nil
}

func (s *Store) q(query string) string {
	if s.driver != "postgres" {
		return query
	}
	var out strings.Builder
	n := 0
	for _, char := range query {
		if char == '?' {
			n++
			fmt.Fprintf(&out, "$%d", n)
		} else {
			out.WriteRune(char)
		}
	}
	return out.String()
}

func (s *Store) begin(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, commandlog.ErrUnavailable
	}
	result, err := tx.ExecContext(ctx, "UPDATE api_command_lock SET revision=revision+1 WHERE id=1")
	if err != nil {
		_ = tx.Rollback()
		return nil, commandlog.ErrUnavailable
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		_ = tx.Rollback()
		return nil, commandlog.ErrUnavailable
	}
	return tx, nil
}

type scanner interface{ Scan(...any) error }

func readEntry(row scanner) (commandlog.Entry, error) {
	var id, actor, connection, operation, keyHash, payload string
	var expires int64
	err := row.Scan(&id, &actor, &connection, &operation, &keyHash, &expires, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return commandlog.Entry{}, commandlog.ErrNotFound
	}
	if err != nil || len(payload) > commandlog.MaxEntryBytes {
		return commandlog.Entry{}, commandlog.ErrUnavailable
	}
	var entry commandlog.Entry
	if json.Unmarshal([]byte(payload), &entry) != nil || !commandlog.ValidEntry(entry) || entry.ID != id || entry.Scope.ActorID != actor || entry.Scope.ConnectionID != connection || entry.Scope.Operation != operation || entry.KeyHash != keyHash || entry.ExpiresAt.UnixMicro() != expires {
		return commandlog.Entry{}, commandlog.ErrUnavailable
	}
	return entry, nil
}

func (s *Store) Reserve(ctx context.Context, scope commandlog.Scope, keyHash, fingerprint string, now time.Time) (commandlog.Entry, bool, error) {
	if !commandlog.ValidReservation(scope, keyHash, fingerprint, now) {
		return commandlog.Entry{}, false, commandlog.ErrInvalid
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return commandlog.Entry{}, false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, s.q("DELETE FROM api_commands WHERE expires_us<=?"), now.UnixMicro()); err != nil {
		return commandlog.Entry{}, false, commandlog.ErrUnavailable
	}
	entry, err := readEntry(tx.QueryRowContext(ctx, s.q("SELECT id,actor_id,connection_id,operation,key_hash,expires_us,payload FROM api_commands WHERE actor_id=? AND connection_id=? AND operation=? AND key_hash=?"), scope.ActorID, scope.ConnectionID, scope.Operation, keyHash))
	if err == nil {
		next, replayErr := commandlog.Replay(entry, fingerprint, now)
		if next.State != entry.State {
			if err = s.write(ctx, tx, next); err != nil {
				return commandlog.Entry{}, false, err
			}
		}
		if err = tx.Commit(); err != nil {
			return commandlog.Entry{}, false, commandlog.ErrUnavailable
		}
		return next, false, replayErr
	}
	if !errors.Is(err, commandlog.ErrNotFound) {
		return commandlog.Entry{}, false, err
	}
	var total, actor int
	if tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM api_commands").Scan(&total) != nil || tx.QueryRowContext(ctx, s.q("SELECT COUNT(*) FROM api_commands WHERE actor_id=?"), scope.ActorID).Scan(&actor) != nil {
		return commandlog.Entry{}, false, commandlog.ErrUnavailable
	}
	if total >= commandlog.MaxRecords || actor >= commandlog.MaxPerActor {
		return commandlog.Entry{}, false, commandlog.ErrLimit
	}
	entry = commandlog.NewEntry(scope, keyHash, fingerprint, now)
	payload, err := json.Marshal(entry)
	if err != nil || len(payload) > commandlog.MaxEntryBytes {
		return commandlog.Entry{}, false, commandlog.ErrInvalid
	}
	if _, err = tx.ExecContext(ctx, s.q("INSERT INTO api_commands(id,actor_id,connection_id,operation,key_hash,expires_us,payload) VALUES(?,?,?,?,?,?,?)"), entry.ID, scope.ActorID, scope.ConnectionID, scope.Operation, keyHash, entry.ExpiresAt.UnixMicro(), string(payload)); err != nil {
		return commandlog.Entry{}, false, commandlog.ErrUnavailable
	}
	if tx.Commit() != nil {
		return commandlog.Entry{}, false, commandlog.ErrUnavailable
	}
	return entry, true, nil
}

func (s *Store) write(ctx context.Context, tx *sql.Tx, entry commandlog.Entry) error {
	payload, err := json.Marshal(entry)
	if err != nil || len(payload) > commandlog.MaxEntryBytes {
		return commandlog.ErrInvalid
	}
	result, err := tx.ExecContext(ctx, s.q("UPDATE api_commands SET payload=? WHERE id=? AND actor_id=? AND connection_id=? AND operation=?"), string(payload), entry.ID, entry.Scope.ActorID, entry.Scope.ConnectionID, entry.Scope.Operation)
	if err != nil {
		return commandlog.ErrUnavailable
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return commandlog.ErrUnavailable
	}
	return nil
}

func (s *Store) Finish(ctx context.Context, id string, scope commandlog.Scope, response *commandlog.Response) error {
	if id == "" || !scope.Valid() || response != nil && !response.Valid() {
		return commandlog.ErrInvalid
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	entry, err := readEntry(tx.QueryRowContext(ctx, s.q("SELECT id,actor_id,connection_id,operation,key_hash,expires_us,payload FROM api_commands WHERE id=? AND actor_id=? AND connection_id=? AND operation=?"), id, scope.ActorID, scope.ConnectionID, scope.Operation))
	if err != nil {
		return err
	}
	if entry.State != commandlog.Pending {
		return commandlog.ErrConflict
	}
	entry.State, entry.Response = commandlog.Unknown, nil
	if response != nil {
		entry.State, entry.Response = commandlog.Completed, response
	}
	if err = s.write(ctx, tx, entry); err != nil {
		return err
	}
	if tx.Commit() != nil {
		return commandlog.ErrUnavailable
	}
	return nil
}

func (s *Store) Ping(ctx context.Context) error {
	if s.db.PingContext(ctx) != nil {
		return commandlog.ErrUnavailable
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

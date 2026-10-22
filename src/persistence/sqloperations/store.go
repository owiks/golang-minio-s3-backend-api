// Package sqloperations persists bounded operation manifests and execution
// leases. The lock row makes admission and claims atomic in all SQL dialects.
package sqloperations

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
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
)

type Config = database.Config
type Store struct {
	db     *sql.DB
	driver string
}

const columns = "id,actor_id,connection_id,state,revision,created_us,expires_us,lease_us,last_served_us,payload"

func Open(ctx context.Context, cfg Config) (*Store, error) {
	db, err := database.Open(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err = migrations.Check(ctx, db, cfg.Driver); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db, driver: cfg.Driver}
	rows, err := db.QueryContext(ctx, "SELECT "+columns+" FROM api_operations WHERE 1=0")
	if err != nil {
		_ = db.Close()
		return nil, operations.ErrUnavailable
	}
	if rows.Close() != nil {
		_ = db.Close()
		return nil, operations.ErrUnavailable
	}
	var id int
	if db.QueryRowContext(ctx, "SELECT id FROM api_operation_lock WHERE id=1").Scan(&id) != nil || id != 1 {
		_ = db.Close()
		return nil, operations.ErrUnavailable
	}
	return s, nil
}
func (s *Store) q(v string) string {
	if s.driver != "postgres" {
		return v
	}
	var b strings.Builder
	n := 0
	for _, r := range v {
		if r == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func (s *Store) begin(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, operations.ErrUnavailable
	}
	result, err := tx.ExecContext(ctx, "UPDATE api_operation_lock SET revision=revision+1 WHERE id=1")
	if err != nil {
		_ = tx.Rollback()
		return nil, operations.ErrUnavailable
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		_ = tx.Rollback()
		return nil, operations.ErrUnavailable
	}
	return tx, nil
}

type rowScanner interface{ Scan(...any) error }

func read(row rowScanner) (operations.Snapshot, error) {
	var id, actor, connection, state, payload string
	var revision uint64
	var created, expires, lease, served int64
	err := row.Scan(&id, &actor, &connection, &state, &revision, &created, &expires, &lease, &served, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return operations.Snapshot{}, operations.ErrNotFound
	}
	if err != nil || len(payload) > operations.MaxRecordBytes {
		return operations.Snapshot{}, operations.ErrUnavailable
	}
	var v operations.Snapshot
	if json.Unmarshal([]byte(payload), &v) != nil || !operations.Valid(v) || v.ID != id || v.Scope.ActorID != actor || v.Scope.ConnectionID != connection || v.State != state || v.Revision != revision || micros(v.CreatedAt) != created || micros(v.ExpiresAt) != expires || micros(v.LeaseUntil) != lease || micros(v.LastServedAt) != served {
		return operations.Snapshot{}, operations.ErrUnavailable
	}
	return v, nil
}
func micros(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMicro()
}
func (s *Store) write(ctx context.Context, tx *sql.Tx, v operations.Snapshot, insert bool) error {
	if !operations.Valid(v) {
		return operations.ErrInvalid
	}
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > operations.MaxRecordBytes {
		return operations.ErrInvalid
	}
	args := []any{v.Scope.ActorID, v.Scope.ConnectionID, v.State, v.Revision, micros(v.CreatedAt), micros(v.ExpiresAt), micros(v.LeaseUntil), micros(v.LastServedAt), string(raw), v.ID}
	query := "UPDATE api_operations SET actor_id=?,connection_id=?,state=?,revision=?,created_us=?,expires_us=?,lease_us=?,last_served_us=?,payload=? WHERE id=?"
	if insert {
		query = "INSERT INTO api_operations(actor_id,connection_id,state,revision,created_us,expires_us,lease_us,last_served_us,payload,id) VALUES(?,?,?,?,?,?,?,?,?,?)"
	}
	r, err := tx.ExecContext(ctx, s.q(query), args...)
	if err != nil {
		return operations.ErrUnavailable
	}
	n, err := r.RowsAffected()
	if err != nil || n != 1 {
		return operations.ErrUnavailable
	}
	return nil
}
func (s *Store) prune(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, s.q("DELETE FROM api_operations WHERE expires_us>0 AND expires_us<=?"), now.UnixMicro())
	if err != nil {
		return operations.ErrUnavailable
	}
	return nil
}
func (s *Store) Insert(ctx context.Context, scope operations.Scope, spec operations.Spec, deadline, now time.Time) (operations.Snapshot, error) {
	v, err := operations.New(scope, spec, deadline, now)
	if err != nil {
		return operations.Snapshot{}, err
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return operations.Snapshot{}, err
	}
	defer tx.Rollback()
	if err = s.prune(ctx, tx, now); err != nil {
		return operations.Snapshot{}, err
	}
	var total, actor int
	if tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM api_operations").Scan(&total) != nil || tx.QueryRowContext(ctx, s.q("SELECT COUNT(*) FROM api_operations WHERE actor_id=?"), scope.ActorID).Scan(&actor) != nil {
		return operations.Snapshot{}, operations.ErrUnavailable
	}
	for actor >= operations.MaxPerActor {
		if err = s.evict(ctx, tx, scope.ActorID); err != nil {
			return operations.Snapshot{}, err
		}
		actor--
		total--
	}
	for total >= operations.MaxRecords {
		if err = s.evict(ctx, tx, ""); err != nil {
			return operations.Snapshot{}, err
		}
		total--
	}
	if err = s.write(ctx, tx, v, true); err != nil {
		return operations.Snapshot{}, err
	}
	if tx.Commit() != nil {
		return operations.Snapshot{}, operations.ErrUnavailable
	}
	return v.Clone(), nil
}
func (s *Store) Get(ctx context.Context, scope operations.Scope, id string) (operations.Snapshot, error) {
	return read(s.db.QueryRowContext(ctx, s.q("SELECT "+columns+" FROM api_operations WHERE id=? AND actor_id=? AND connection_id=?"), id, scope.ActorID, scope.ConnectionID))
}
func (s *Store) List(ctx context.Context, scope operations.Scope) ([]operations.Snapshot, error) {
	rows, err := s.db.QueryContext(ctx, s.q("SELECT "+columns+" FROM api_operations WHERE actor_id=? AND connection_id=? ORDER BY created_us DESC,id"), scope.ActorID, scope.ConnectionID)
	if err != nil {
		return nil, operations.ErrUnavailable
	}
	defer rows.Close()
	out := []operations.Snapshot{}
	for rows.Next() {
		v, err := read(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		if len(out) > operations.MaxPerActor {
			return nil, operations.ErrUnavailable
		}
	}
	if rows.Err() != nil {
		return nil, operations.ErrUnavailable
	}
	return out, nil
}
func (s *Store) Claim(ctx context.Context, worker string, now time.Time, lease time.Duration) (operations.Snapshot, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return operations.Snapshot{}, err
	}
	defer tx.Rollback()
	if err = s.prune(ctx, tx, now); err != nil {
		return operations.Snapshot{}, err
	}
	for range operations.MaxRecords {
		// Load a single manifest, not every potentially large queued payload.
		query := "SELECT " + strings.ReplaceAll(columns, ",", ",o.") + " FROM api_operations o WHERE (o.state='queued' OR (o.state='running' AND o.lease_us<=?)) ORDER BY COALESCE((SELECT MAX(a.last_served_us) FROM api_operations a WHERE a.actor_id=o.actor_id),0),o.created_us,o.id LIMIT 1"
		query = strings.Replace(query, "SELECT id,", "SELECT o.id,", 1)
		v, err := read(tx.QueryRowContext(ctx, s.q(query), now.UnixMicro()))
		if errors.Is(err, operations.ErrNotFound) {
			if tx.Commit() != nil {
				return operations.Snapshot{}, operations.ErrUnavailable
			}
			return operations.Snapshot{}, operations.ErrNotFound
		}
		if err != nil {
			return operations.Snapshot{}, err
		}
		records := map[string]operations.Snapshot{v.ID: v}
		claimed, claimErr := operations.ClaimRecord(records, worker, now, lease)
		if claimErr != nil && !errors.Is(claimErr, operations.ErrNotFound) {
			return operations.Snapshot{}, claimErr
		}
		if err = s.write(ctx, tx, records[v.ID], false); err != nil {
			return operations.Snapshot{}, err
		}
		if claimErr == nil {
			if tx.Commit() != nil {
				return operations.Snapshot{}, operations.ErrUnavailable
			}
			return claimed, nil
		}
	}
	if tx.Commit() != nil {
		return operations.Snapshot{}, operations.ErrUnavailable
	}
	return operations.Snapshot{}, operations.ErrNotFound
}
func (s *Store) mutate(ctx context.Context, id string, fn func(operations.Snapshot) (operations.Snapshot, error)) (operations.Snapshot, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return operations.Snapshot{}, err
	}
	defer tx.Rollback()
	v, err := read(tx.QueryRowContext(ctx, s.q("SELECT "+columns+" FROM api_operations WHERE id=?"), id))
	if err != nil {
		return operations.Snapshot{}, err
	}
	previous := v.Clone()
	v, err = fn(v)
	if err != nil {
		return operations.Snapshot{}, err
	}
	if !operations.Same(previous, v) {
		if err = s.write(ctx, tx, v, false); err != nil {
			return operations.Snapshot{}, err
		}
	}
	if tx.Commit() != nil {
		return operations.Snapshot{}, operations.ErrUnavailable
	}
	return v.Clone(), nil
}
func (s *Store) Save(ctx context.Context, id, lease string, rev uint64, c operations.Checkpoint, n time.Time) (operations.Snapshot, error) {
	return s.mutate(ctx, id, func(v operations.Snapshot) (operations.Snapshot, error) {
		return operations.SaveRecord(v, lease, rev, c, n)
	})
}
func (s *Store) Renew(ctx context.Context, id, lease string, n time.Time, l time.Duration) error {
	_, err := s.mutate(ctx, id, func(v operations.Snapshot) (operations.Snapshot, error) {
		if v.State != operations.Running || v.LeaseID != lease || !n.Before(v.LeaseUntil) {
			return v, operations.ErrConflict
		}
		if l < time.Second || l > time.Hour {
			return v, operations.ErrInvalid
		}
		v.LeaseUntil = n.Add(l).UTC().Truncate(time.Microsecond)
		return v, nil
	})
	return err
}
func (s *Store) Cancel(ctx context.Context, scope operations.Scope, id string, rev uint64, n time.Time) (operations.Snapshot, error) {
	return s.mutate(ctx, id, func(v operations.Snapshot) (operations.Snapshot, error) {
		if v.Scope != scope {
			return v, operations.ErrNotFound
		}
		return operations.CancelRecord(v, rev, n)
	})
}
func (s *Store) Retry(ctx context.Context, scope operations.Scope, id string, rev uint64, n time.Time) (operations.Snapshot, error) {
	return s.mutate(ctx, id, func(v operations.Snapshot) (operations.Snapshot, error) {
		if v.Scope != scope {
			return v, operations.ErrNotFound
		}
		return operations.RetryRecord(v, rev, n)
	})
}
func (s *Store) Prune(ctx context.Context, n time.Time) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.prune(ctx, tx, n); err != nil {
		return err
	}
	if tx.Commit() != nil {
		return operations.ErrUnavailable
	}
	return nil
}
func (s *Store) Ping(ctx context.Context) error {
	if s.db.PingContext(ctx) != nil {
		return operations.ErrUnavailable
	}
	return nil
}
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) evict(ctx context.Context, tx *sql.Tx, actor string) error {
	query := "SELECT id FROM api_operations WHERE state NOT IN ('queued','running')"
	args := []any{}
	if actor != "" {
		query += " AND actor_id=?"
		args = append(args, actor)
	}
	query += " ORDER BY expires_us,id LIMIT 1"
	var id string
	if err := tx.QueryRowContext(ctx, s.q(query), args...).Scan(&id); errors.Is(err, sql.ErrNoRows) {
		return operations.ErrLimit
	} else if err != nil {
		return operations.ErrUnavailable
	}
	if _, err := tx.ExecContext(ctx, s.q("DELETE FROM api_operations WHERE id=?"), id); err != nil {
		return operations.ErrUnavailable
	}
	return nil
}
func (s *Store) Export(ctx context.Context) ([]operations.Snapshot, error) {
	// Export is used only in the documented offline state transfer. A lock still
	// protects a coherent snapshot from concurrent repository users.
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT "+columns+" FROM api_operations ORDER BY id")
	if err != nil {
		return nil, operations.ErrUnavailable
	}
	out := []operations.Snapshot{}
	size := 0
	for rows.Next() {
		v, readErr := read(rows)
		if readErr != nil {
			_ = rows.Close()
			return nil, readErr
		}
		raw, encodeErr := json.Marshal(v)
		if encodeErr != nil {
			_ = rows.Close()
			return nil, operations.ErrUnavailable
		}
		if len(raw) > operations.MaxSnapshotBytes-size {
			_ = rows.Close()
			return nil, operations.ErrLimit
		}
		size += len(raw)
		out = append(out, v)
		if len(out) > operations.MaxRecords {
			_ = rows.Close()
			return nil, operations.ErrUnavailable
		}
	}
	if rows.Err() != nil {
		_ = rows.Close()
		return nil, operations.ErrUnavailable
	}
	if rows.Close() != nil {
		return nil, operations.ErrUnavailable
	}
	if tx.Commit() != nil {
		return nil, operations.ErrUnavailable
	}
	return out, nil
}
func (s *Store) Import(ctx context.Context, records []operations.Snapshot) error {
	if !operations.ValidBackup(records) {
		return operations.ErrInvalid
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	incoming := map[string]operations.Snapshot{}
	for _, v := range records {
		incoming[v.ID] = v
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+columns+" FROM api_operations")
	if err != nil {
		return operations.ErrUnavailable
	}
	existing := map[string]bool{}
	for rows.Next() {
		old, readErr := read(rows)
		if readErr != nil {
			_ = rows.Close()
			return readErr
		}
		next, ok := incoming[old.ID]
		if !ok || !operations.Same(old, next) {
			_ = rows.Close()
			return operations.ErrConflict
		}
		existing[old.ID] = true
	}
	if rows.Err() != nil {
		_ = rows.Close()
		return operations.ErrUnavailable
	}
	if rows.Close() != nil {
		return operations.ErrUnavailable
	}
	for _, v := range records {
		if existing[v.ID] {
			continue
		}
		if err = s.write(ctx, tx, v, true); err != nil {
			return err
		}
	}
	if tx.Commit() != nil {
		return operations.ErrUnavailable
	}
	return nil
}

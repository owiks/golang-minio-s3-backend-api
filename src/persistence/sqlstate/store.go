// Package sqlstate implements the same durable access contract for three SQL dialects.
package sqlstate

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
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
)

var ErrOpen = database.ErrOpen

type Config = database.Config
type Store struct {
	db     *sql.DB
	driver string
	now    func() time.Time
}

func Open(ctx context.Context, c Config) (*Store, error) {
	return OpenWithClock(ctx, c, time.Now)
}

// OpenWithClock shares the manager clock for decisions inside write transactions.
// The clock must be safe for concurrent calls. A nil clock uses the wall clock.
func OpenWithClock(ctx context.Context, c Config, now func() time.Time) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	db, err := database.Open(ctx, c)
	if err != nil {
		return nil, err
	}
	if err = migrations.Check(ctx, db, c.Driver); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, driver: c.Driver, now: now}, nil
}
func (s *Store) q(query string) string {
	if s.driver != "postgres" {
		return query
	}
	var out strings.Builder
	n := 0
	for _, c := range query {
		if c == '?' {
			n++
			fmt.Fprintf(&out, "$%d", n)
		} else {
			out.WriteRune(c)
		}
	}
	return out.String()
}
func (s *Store) Ping(ctx context.Context) error {
	if s.db.PingContext(ctx) != nil {
		return authstate.Unavailable()
	}
	return nil
}
func (s *Store) Close() error { return s.db.Close() }

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) records(ctx context.Context, q queryer, owner string) (authstate.Records, error) {
	records := authstate.Records{Connections: map[string]authstate.Connection{}, Accesses: map[string]authstate.Access{}}
	for _, table := range []string{"api_connections", "api_accesses"} {
		query := "SELECT id,payload FROM " + table
		args := []any{}
		if owner != "" {
			query += " WHERE owner=?"
			args = append(args, owner)
		}
		query += " ORDER BY id LIMIT 16385"
		rows, err := q.QueryContext(ctx, s.q(query), args...)
		if err != nil {
			return records, authstate.Unavailable()
		}
		count := 0
		valid := true
		for rows.Next() {
			count++
			var id, payload string
			if rows.Scan(&id, &payload) != nil || len(payload) > 262144 {
				valid = false
				break
			}
			if table == "api_connections" {
				var d authstate.ConnectionData
				if json.Unmarshal([]byte(payload), &d) != nil || d.ID != id || d.Owner == "" || (owner != "" && d.Owner != owner) {
					valid = false
					break
				}
				records.Connections[id] = authstate.NewConnection(d)
			} else {
				var d authstate.AccessData
				if json.Unmarshal([]byte(payload), &d) != nil || d.ID != id || !d.Policy.Valid() || d.Owner == "" || (owner != "" && d.Owner != owner) {
					valid = false
					break
				}
				records.Accesses[id] = authstate.NewAccess(d)
			}
		}
		scanErr := rows.Err()
		closeErr := rows.Close()
		if !valid || scanErr != nil || closeErr != nil || count > 16384 {
			return records, authstate.Unavailable()
		}
	}
	return records, nil
}
func (s *Store) begin(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, authstate.Unavailable()
	}
	if _, err = tx.ExecContext(ctx, "UPDATE api_write_lock SET revision=revision+1 WHERE id=1"); err != nil {
		_ = tx.Rollback()
		return nil, authstate.Unavailable()
	}
	return tx, nil
}
func (s *Store) write(ctx context.Context, tx *sql.Tx, changes authstate.Changes) error {
	for _, c := range changes.Connections {
		d := c.Snapshot()
		raw, err := json.Marshal(d)
		if err != nil {
			return authstate.Unavailable()
		}
		query := "UPDATE api_connections SET payload=? WHERE id=? AND owner=?"
		result, err := tx.ExecContext(ctx, s.q(query), string(raw), d.ID, d.Owner)
		if err != nil {
			return authstate.Unavailable()
		}
		n, err := result.RowsAffected()
		if err != nil || n != 1 {
			return authstate.Unavailable()
		}
	}
	for _, a := range changes.Accesses {
		d := a.Snapshot()
		raw, err := json.Marshal(d)
		if err != nil {
			return authstate.Unavailable()
		}
		result, err := tx.ExecContext(ctx, s.q("UPDATE api_accesses SET payload=? WHERE id=? AND owner=?"), string(raw), d.ID, d.Owner)
		if err != nil {
			return authstate.Unavailable()
		}
		n, err := result.RowsAffected()
		if err != nil {
			return authstate.Unavailable()
		}
		if n == 0 {
			if _, err := tx.ExecContext(ctx, s.q("INSERT INTO api_accesses(id,owner,connection_id,payload) VALUES(?,?,?,?)"), d.ID, d.Owner, d.ConnectionID, string(raw)); err != nil {
				return authstate.Unavailable()
			}
		}
	}
	raw, err := json.Marshal(changes.Event)
	if err != nil {
		return authstate.Unavailable()
	}
	if _, err = tx.ExecContext(ctx, s.q("INSERT INTO api_events(id,owner,at_us,payload) VALUES(?,?,?,?)"), changes.Event.ID, changes.Event.Owner, changes.Event.At.UnixMicro(), string(raw)); err != nil {
		return authstate.Unavailable()
	}
	return s.pruneEvents(ctx, tx)
}
func (s *Store) Bootstrap(ctx context.Context, c authstate.Connection, a authstate.Access, e authstate.Event) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.capacity(ctx, tx, true); err != nil {
		return err
	}
	d := c.Snapshot()
	records, err := s.records(ctx, tx, d.Owner)
	if err != nil {
		return err
	}
	if err = authstate.ValidateBootstrap(records, c, a); err != nil {
		return err
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return authstate.Unavailable()
	}
	if _, err = tx.ExecContext(ctx, s.q("INSERT INTO api_connections(id,owner,payload) VALUES(?,?,?)"), d.ID, d.Owner, string(raw)); err != nil {
		return authstate.Unavailable()
	}
	if err = s.write(ctx, tx, authstate.Changes{Accesses: []authstate.Access{a}, Event: e}); err != nil {
		return err
	}
	if tx.Commit() != nil {
		return authstate.Unavailable()
	}
	return nil
}
func (s *Store) Apply(ctx context.Context, cmd authstate.Command) (authstate.Result, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return authstate.Result{}, err
	}
	defer tx.Rollback()
	if cmd.Kind == "create" {
		if err = s.capacity(ctx, tx, false); err != nil {
			return authstate.Result{}, err
		}
	}
	records, err := s.records(ctx, tx, cmd.Guard.Owner)
	if err != nil {
		return authstate.Result{}, err
	}
	changes, err := authstate.TransitionAt(records, cmd, s.now())
	if err != nil {
		return authstate.Result{}, err
	}
	if err = s.write(ctx, tx, changes); err != nil {
		return authstate.Result{}, err
	}
	if tx.Commit() != nil {
		return authstate.Result{}, authstate.Unavailable()
	}
	return changes.Result, nil
}

func (s *Store) pruneEvents(ctx context.Context, tx *sql.Tx) error {
	var at int64
	var id string
	err := tx.QueryRowContext(ctx, "SELECT at_us,id FROM api_events ORDER BY at_us DESC,id DESC LIMIT 1 OFFSET 9999").Scan(&at, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return authstate.Unavailable()
	}
	if _, err = tx.ExecContext(ctx, s.q("DELETE FROM api_events WHERE at_us<? OR (at_us=? AND id<?)"), at, at, id); err != nil {
		return authstate.Unavailable()
	}
	return nil
}

func (s *Store) capacity(ctx context.Context, tx *sql.Tx, connection bool) error {
	tables := []string{"api_accesses"}
	if connection {
		tables = append(tables, "api_connections")
	}
	for _, table := range tables {
		var count int
		if tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count) != nil {
			return authstate.Unavailable()
		}
		if count >= 16384 {
			return authstate.Limit()
		}
	}
	return nil
}

package sqlstate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
)

func (s *Store) get(ctx context.Context, q queryer, id string) (authstate.Access, authstate.Connection, error) {
	var ar, cr string
	err := q.QueryRowContext(ctx, s.q("SELECT a.payload,c.payload FROM api_accesses a JOIN api_connections c ON a.connection_id=c.id WHERE a.id=?"), id).Scan(&ar, &cr)
	if errors.Is(err, sql.ErrNoRows) {
		return authstate.Access{}, authstate.Connection{}, authstate.NotFound()
	}
	if err != nil || len(ar) > 262144 || len(cr) > 262144 {
		return authstate.Access{}, authstate.Connection{}, authstate.Unavailable()
	}
	var a authstate.AccessData
	var c authstate.ConnectionData
	if json.Unmarshal([]byte(ar), &a) != nil || json.Unmarshal([]byte(cr), &c) != nil || !authstate.ValidAccess(a) || !authstate.ValidConnection(c) || a.ID != id || a.ConnectionID != c.ID || a.Owner != c.Owner || !a.Policy.Valid() || a.Revision < 1 || a.Generation < 1 || len(a.Digest) != 64 || (a.Mode != "cookie" && a.Mode != "bearer") {
		return authstate.Access{}, authstate.Connection{}, authstate.Unavailable()
	}
	return authstate.NewAccess(a), authstate.NewConnection(c), nil
}
func (s *Store) Get(ctx context.Context, id string) (authstate.Access, authstate.Connection, error) {
	a, c, err := s.get(ctx, s.db, id)
	if err != nil {
		return authstate.Access{}, authstate.Connection{}, err
	}
	if a.Snapshot().ParentID == "" {
		if err = authstate.ValidateLineage(a, c, nil); err != nil {
			return authstate.Access{}, authstate.Connection{}, err
		}
		return a, c, nil
	}
	// Ancestors and the grant must be read in one snapshot: otherwise a
	// concurrent narrowing/revocation could mix old child and new parent data.
	// Roots keep the single-query path because their parent is immutable.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return authstate.Access{}, authstate.Connection{}, authstate.Unavailable()
	}
	defer tx.Rollback()
	a, c, err = s.get(ctx, tx, id)
	if err != nil {
		return authstate.Access{}, authstate.Connection{}, err
	}
	lookup := func(parentID string) (authstate.Access, authstate.Connection, error) {
		return s.get(ctx, tx, parentID)
	}
	if err = authstate.ValidateLineage(a, c, lookup); err != nil {
		return authstate.Access{}, authstate.Connection{}, err
	}
	if err = tx.Commit(); err != nil {
		return authstate.Access{}, authstate.Connection{}, authstate.Unavailable()
	}
	return a, c, nil
}
func (s *Store) Connections(ctx context.Context, owner string) ([]authstate.Connection, error) {
	rows, err := s.db.QueryContext(ctx, s.q("SELECT payload FROM api_connections WHERE owner=? ORDER BY id LIMIT 16385"), owner)
	if err != nil {
		return nil, authstate.Unavailable()
	}
	defer rows.Close()
	out := []authstate.Connection{}
	for rows.Next() {
		var raw string
		var d authstate.ConnectionData
		if rows.Scan(&raw) != nil || len(raw) > 262144 || json.Unmarshal([]byte(raw), &d) != nil || d.Owner != owner {
			return nil, authstate.Unavailable()
		}
		out = append(out, authstate.NewConnection(d))
	}
	if rows.Err() != nil || len(out) > 16384 {
		return nil, authstate.Unavailable()
	}
	return out, nil
}
func (s *Store) Accesses(ctx context.Context, owner, cid string) ([]authstate.Access, error) {
	var found string
	if err := s.db.QueryRowContext(ctx, s.q("SELECT id FROM api_connections WHERE id=? AND owner=?"), cid, owner).Scan(&found); errors.Is(err, sql.ErrNoRows) {
		return nil, authstate.NotFound()
	} else if err != nil {
		return nil, authstate.Unavailable()
	}
	rows, err := s.db.QueryContext(ctx, s.q("SELECT payload FROM api_accesses WHERE owner=? AND connection_id=? ORDER BY id LIMIT 16385"), owner, cid)
	if err != nil {
		return nil, authstate.Unavailable()
	}
	defer rows.Close()
	out := []authstate.Access{}
	for rows.Next() {
		var raw string
		var d authstate.AccessData
		if rows.Scan(&raw) != nil || len(raw) > 262144 || json.Unmarshal([]byte(raw), &d) != nil || d.Owner != owner || d.ConnectionID != cid || !d.Policy.Valid() {
			return nil, authstate.Unavailable()
		}
		out = append(out, authstate.NewAccess(d))
	}
	if rows.Err() != nil || len(out) > 16384 {
		return nil, authstate.Unavailable()
	}
	return out, nil
}
func (s *Store) events(ctx context.Context, q queryer, owner string) ([]authstate.Event, error) {
	query := "SELECT payload FROM api_events"
	args := []any{}
	if owner != "" {
		query += " WHERE owner=?"
		args = append(args, owner)
	}
	query += " ORDER BY at_us DESC,id DESC LIMIT 10000"
	rows, err := q.QueryContext(ctx, s.q(query), args...)
	if err != nil {
		return nil, authstate.Unavailable()
	}
	defer rows.Close()
	out := []authstate.Event{}
	for rows.Next() {
		var raw string
		var e authstate.Event
		if rows.Scan(&raw) != nil || len(raw) > 65536 || json.Unmarshal([]byte(raw), &e) != nil || (owner != "" && e.Owner != owner) {
			return nil, authstate.Unavailable()
		}
		out = append(out, e)
	}
	if rows.Err() != nil {
		return nil, authstate.Unavailable()
	}
	return out, nil
}
func (s *Store) Events(ctx context.Context, owner string) ([]authstate.Event, error) {
	return s.events(ctx, s.db, owner)
}
func (s *Store) Touch(ctx context.Context, id string, now time.Time) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a, _, err := s.get(ctx, tx, id)
	if err != nil {
		return err
	}
	d := a.Snapshot()
	if a.Valid(now) && now.Sub(d.LastUsedAt) >= time.Minute {
		d.LastUsedAt = now
		raw, err := json.Marshal(d)
		if err != nil {
			return authstate.Unavailable()
		}
		if _, err = tx.ExecContext(ctx, s.q("UPDATE api_accesses SET payload=? WHERE id=?"), string(raw), id); err != nil {
			return authstate.Unavailable()
		}
	}
	if tx.Commit() != nil {
		return authstate.Unavailable()
	}
	return nil
}
func (s *Store) Reencrypt(ctx context.Context, c authstate.Connection) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	d := c.Snapshot()
	var raw string
	if err = tx.QueryRowContext(ctx, s.q("SELECT payload FROM api_connections WHERE id=? AND owner=?"), d.ID, d.Owner).Scan(&raw); err != nil {
		return authstate.Unavailable()
	}
	var before authstate.ConnectionData
	if json.Unmarshal([]byte(raw), &before) != nil {
		return authstate.Unavailable()
	}
	if before.Revision != d.Revision || before.SecretVersion != d.SecretVersion {
		return authstate.Conflict()
	}
	before.Cipher = d.Cipher
	before.Revision++
	if err = s.write(ctx, tx, authstate.Changes{Connections: []authstate.Connection{authstate.NewConnection(before)}, Event: authstate.ReencryptionEvent(c)}); err != nil {
		return err
	}
	if tx.Commit() != nil {
		return authstate.Unavailable()
	}
	return nil
}

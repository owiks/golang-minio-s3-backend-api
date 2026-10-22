package sqlstate

import (
	"context"
	"encoding/json"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
)

func (s *Store) Export(ctx context.Context) (authstate.Backup, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return authstate.Backup{}, err
	}
	defer tx.Rollback()
	records, err := s.records(ctx, tx, "")
	if err != nil {
		return authstate.Backup{}, err
	}
	events, err := s.events(ctx, tx, "")
	if err != nil {
		return authstate.Backup{}, err
	}
	b := authstate.Backup{Version: 1, Connections: []authstate.ConnectionData{}, Accesses: []authstate.AccessData{}, Events: events}
	for _, c := range records.Connections {
		b.Connections = append(b.Connections, c.Snapshot())
	}
	for _, a := range records.Accesses {
		b.Accesses = append(b.Accesses, a.Snapshot())
	}
	return b, nil
}
func (s *Store) Import(ctx context.Context, b authstate.Backup) error {
	if err := authstate.ValidateBackup(b); err != nil {
		return err
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if tx.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM api_connections)+(SELECT COUNT(*) FROM api_accesses)+(SELECT COUNT(*) FROM api_events)").Scan(&count) != nil {
		return authstate.Unavailable()
	}
	if count != 0 {
		return authstate.Conflict()
	}
	for _, c := range b.Connections {
		raw, err := json.Marshal(c)
		if err != nil {
			return authstate.Invalid()
		}
		if _, err = tx.ExecContext(ctx, s.q("INSERT INTO api_connections(id,owner,payload) VALUES(?,?,?)"), c.ID, c.Owner, string(raw)); err != nil {
			return authstate.Unavailable()
		}
	}
	for _, a := range b.Accesses {
		raw, err := json.Marshal(a)
		if err != nil {
			return authstate.Invalid()
		}
		if _, err = tx.ExecContext(ctx, s.q("INSERT INTO api_accesses(id,owner,connection_id,payload) VALUES(?,?,?,?)"), a.ID, a.Owner, a.ConnectionID, string(raw)); err != nil {
			return authstate.Unavailable()
		}
	}
	for _, e := range b.Events {
		raw, err := json.Marshal(e)
		if err != nil {
			return authstate.Invalid()
		}
		if _, err = tx.ExecContext(ctx, s.q("INSERT INTO api_events(id,owner,at_us,payload) VALUES(?,?,?,?)"), e.ID, e.Owner, e.At.UnixMicro(), string(raw)); err != nil {
			return authstate.Unavailable()
		}
	}
	if tx.Commit() != nil {
		return authstate.Unavailable()
	}
	return nil
}

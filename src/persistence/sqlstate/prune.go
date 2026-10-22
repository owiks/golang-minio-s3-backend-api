package sqlstate

import (
	"context"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
)

func (s *Store) Prune(ctx context.Context, before time.Time) (int, error) {
	if before.IsZero() || !before.Before(s.now()) {
		return 0, authstate.Invalid()
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := s.records(ctx, tx, "")
	if err != nil {
		return 0, err
	}
	accesses, connections := authstate.Prunable(rows, before)
	for _, id := range accesses {
		if _, err = tx.ExecContext(ctx, s.q("DELETE FROM api_accesses WHERE id=?"), id); err != nil {
			return 0, authstate.Unavailable()
		}
	}
	for _, id := range connections {
		if _, err = tx.ExecContext(ctx, s.q("DELETE FROM api_connections WHERE id=?"), id); err != nil {
			return 0, authstate.Unavailable()
		}
	}
	if tx.Commit() != nil {
		return 0, authstate.Unavailable()
	}
	return len(accesses) + len(connections), nil
}

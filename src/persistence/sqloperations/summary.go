package sqloperations

import (
	"context"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
)

// Summary reads indexed metadata only. Unlike Export it neither locks writers
// nor reads private, potentially large manifests into operator responses.
func (s *Store) Summary(ctx context.Context, now time.Time) (operations.QueueSummary, error) {
	rows, err := s.db.QueryContext(ctx, s.q("SELECT state,COUNT(*),MIN(created_us),SUM(CASE WHEN state='running' AND lease_us<=? THEN 1 ELSE 0 END) FROM api_operations WHERE expires_us=0 OR expires_us>? GROUP BY state"), now.UnixMicro(), now.UnixMicro())
	if err != nil {
		return operations.QueueSummary{}, operations.ErrUnavailable
	}
	defer rows.Close()
	result := operations.QueueSummary{Scope: "store"}
	for rows.Next() {
		var state string
		var count, expired int
		var oldest int64
		if rows.Scan(&state, &count, &oldest, &expired) != nil || result.AddMetadata(state, count, expired, time.UnixMicro(oldest), now) != nil {
			return operations.QueueSummary{}, operations.ErrUnavailable
		}
	}
	if rows.Err() != nil || rows.Close() != nil {
		return operations.QueueSummary{}, operations.ErrUnavailable
	}
	return result, nil
}

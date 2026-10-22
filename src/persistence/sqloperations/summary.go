package sqloperations

import (
	"context"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
)

// Summary reads indexed metadata only. Unlike Export it neither locks writers
// nor reads private, potentially large manifests into operator responses.
func (s *Store) Summary(ctx context.Context, now time.Time) (operations.QueueSummary, error) {
	rows, err := s.db.QueryContext(ctx, s.q("SELECT o.state,COUNT(*),MIN(o.created_us),SUM(CASE WHEN o.state='running' AND o.lease_us<=? THEN 1 ELSE 0 END),SUM(CASE WHEN o.state='queued' AND EXISTS (SELECT 1 FROM api_operations c WHERE c.connection_id=o.connection_id AND c.not_before_us>?) THEN 1 ELSE 0 END) FROM api_operations o WHERE o.expires_us=0 OR o.expires_us>? GROUP BY o.state"), now.UnixMicro(), now.UnixMicro(), now.UnixMicro())
	if err != nil {
		return operations.QueueSummary{}, operations.ErrUnavailable
	}
	defer rows.Close()
	result := operations.QueueSummary{Scope: "store"}
	for rows.Next() {
		var state string
		var count, expired, throttled int
		var oldest int64
		if rows.Scan(&state, &count, &oldest, &expired, &throttled) != nil || result.AddMetadata(state, count, expired, time.UnixMicro(oldest), now) != nil || throttled < 0 || throttled > count || state != operations.Queued && throttled != 0 {
			return operations.QueueSummary{}, operations.ErrUnavailable
		}
		result.Throttled += throttled
	}
	if rows.Err() != nil || rows.Close() != nil {
		return operations.QueueSummary{}, operations.ErrUnavailable
	}
	return result, nil
}

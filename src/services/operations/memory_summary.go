package operations

import (
	"context"
	"time"
)

func (m *Memory) Summary(ctx context.Context, now time.Time) (QueueSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return QueueSummary{}, err
	}
	summary := QueueSummary{Scope: "store"}
	cooldowns := map[string]time.Time{}
	for _, row := range m.records {
		if row.NotBefore.After(cooldowns[row.Scope.ConnectionID]) {
			cooldowns[row.Scope.ConnectionID] = row.NotBefore
		}
	}
	for _, row := range m.records {
		if !row.ExpiresAt.IsZero() && !now.Before(row.ExpiresAt) {
			continue
		}
		expired := 0
		if row.State == Running && !now.Before(row.LeaseUntil) {
			expired = 1
		}
		if err := summary.AddMetadata(row.State, 1, expired, row.CreatedAt, now); err != nil {
			return QueueSummary{}, err
		}
		if row.State == Queued && cooldowns[row.Scope.ConnectionID].After(now) {
			summary.Throttled++
		}
	}
	return summary, nil
}

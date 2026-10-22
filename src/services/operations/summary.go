package operations

import (
	"context"
	"time"
)

// SummaryReader exposes aggregates only. It must not decode or return operation
// manifests, actors, credentials, object names or execution fences.
type SummaryReader interface {
	Summary(context.Context, time.Time) (QueueSummary, error)
}

type QueueSummary struct {
	Scope              string     `json:"scope"`
	Queued             int        `json:"queued"`
	Running            int        `json:"running"`
	ExpiredLeases      int        `json:"expiredLeases"`
	Completed          int        `json:"completed"`
	Failed             int        `json:"failed"`
	Cancelled          int        `json:"cancelled"`
	Unknown            int        `json:"unknown"`
	OldestQueuedAt     *time.Time `json:"oldestQueuedAt,omitempty"`
	OldestQueuedAgeSec int64      `json:"oldestQueuedAgeSeconds"`
}

// AddMetadata shares aggregation rules across the memory and SQL adapters. No
// state transition, expired-lease recovery or pruning is performed by a read.
func (s *QueueSummary) AddMetadata(state string, count, expired int, oldest, now time.Time) error {
	if count < 0 || count > MaxRecords || expired < 0 || expired > count || s.total() > MaxRecords-count {
		return ErrUnavailable
	}
	s.Scope = "store"
	switch state {
	case Queued:
		s.Queued += count
		if count > 0 && oldest.IsZero() {
			return ErrUnavailable
		}
		if count > 0 && (s.OldestQueuedAt == nil || oldest.Before(*s.OldestQueuedAt)) {
			oldest = normalized(oldest)
			s.OldestQueuedAt = &oldest
			s.OldestQueuedAgeSec = max(0, int64(now.Sub(oldest)/time.Second))
		}
	case Running:
		s.Running += count
		s.ExpiredLeases += expired
	case Completed:
		s.Completed += count
	case Failed:
		s.Failed += count
	case Cancelled:
		s.Cancelled += count
	case Unknown:
		s.Unknown += count
	default:
		return ErrUnavailable
	}
	return nil
}

func (s QueueSummary) total() int {
	return s.Queued + s.Running + s.Completed + s.Failed + s.Cancelled + s.Unknown
}

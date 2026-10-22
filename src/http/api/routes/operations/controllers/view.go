package controllers

import (
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
)

// View is deliberately separate from the persisted snapshot: checkpoints,
// execution leases and credential ownership never become public response fields.
type View struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	State string `json:"state"`
	// Phase is a coarse observed stage, not an ETA or queue position.
	Phase         string `json:"phase"`
	WaitingReason string `json:"waitingReason,omitempty"`
	// Timing is omitted for legacy records whose execution was not measured.
	Timing          *TimingView `json:"timing,omitempty"`
	Code            string      `json:"code,omitempty"`
	Revision        uint64      `json:"revision"`
	CreatedAt       time.Time   `json:"createdAt"`
	UpdatedAt       time.Time   `json:"updatedAt"`
	ExpiresAt       *time.Time  `json:"expiresAt,omitempty"`
	Total           int         `json:"total,omitempty"`
	Completed       int         `json:"completed"`
	Errors          int         `json:"errors"`
	Items           []ItemView  `json:"items,omitempty"`
	Scan            *ScanView   `json:"scan,omitempty"`
	CancelRequested bool        `json:"cancelRequested"`
	Retryable       bool        `json:"retryable"`
}
type ItemView struct {
	Index  int    `json:"index"`
	State  string `json:"state"`
	Copied bool   `json:"copied"`
	Moved  bool   `json:"moved"`
	Code   string `json:"code,omitempty"`
}
type ScanView struct {
	Objects int64 `json:"objects"`
	Bytes   int64 `json:"bytes"`
	Pages   int   `json:"pages"`
}
type TimingView struct {
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	WaitingMS   int64      `json:"waitingMs"`
	ExecutionMS int64      `json:"executionMs"`
}

func publicView(s operations.Snapshot, now time.Time) View {
	v := View{ID: s.ID, Kind: s.Kind, State: s.State, Code: s.Code, Revision: s.Revision, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, Total: len(s.Spec.Transfers), Completed: s.NextIndex, CancelRequested: s.CancelRequested, Retryable: s.Retryable() && now.Before(s.Deadline)}
	v.Phase, v.WaitingReason = s.Phase()
	if waiting, execution, known := s.Durations(now); known {
		v.Timing = &TimingView{WaitingMS: waiting.Milliseconds(), ExecutionMS: execution.Milliseconds()}
		if !s.Timing.StartedAt.IsZero() {
			started := s.Timing.StartedAt
			v.Timing.StartedAt = &started
		}
		if !s.Timing.FinishedAt.IsZero() {
			finished := s.Timing.FinishedAt
			v.Timing.FinishedAt = &finished
		}
	}
	if !s.ExpiresAt.IsZero() {
		expires := s.ExpiresAt
		v.ExpiresAt = &expires
	}
	for _, item := range s.Items {
		v.Items = append(v.Items, ItemView{Index: item.Index, State: item.State, Copied: item.Copied, Moved: item.Moved, Code: item.Code})
		if item.State != "success" {
			v.Errors++
		}
	}
	if s.Spec.Scan != nil {
		v.Scan = &ScanView{Objects: s.Scan.Objects, Bytes: s.Scan.Bytes, Pages: s.Scan.Pages}
	}
	return v
}

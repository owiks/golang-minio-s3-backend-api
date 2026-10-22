package operations

import "time"

// OperatorStatus deliberately separates shared queue metadata from observations
// of the process that answered the request. It contains no operation identity.
type OperatorStatus struct {
	CheckedAt time.Time    `json:"checkedAt"`
	Queue     QueueSummary `json:"queue"`
	Worker    WorkerStatus `json:"worker"`
	Policy    PolicyStatus `json:"policy"`
}

type PolicyStatus struct {
	ReadOnly            bool `json:"readOnly"`
	DisableUploads      bool `json:"disableUploads"`
	DisableDeletes      bool `json:"disableDeletes"`
	DisableAdminChanges bool `json:"disableAdminChanges"`
}

// ValidStatus bounds untrusted operator HTTP responses before the CLI displays
// them. Unknown JSON fields are discarded by decoding into the explicit DTO.
func ValidStatus(s OperatorStatus) bool {
	if s.CheckedAt.IsZero() || s.Queue.Scope != "store" || s.Worker.Scope != "process" || s.Worker.Slots < 0 || s.Worker.Slots > 16 || s.Worker.Active < 0 || s.Worker.Active > s.Worker.Slots || s.Queue.total() > MaxRecords || s.Queue.OldestQueuedAgeSec < 0 || !safeCode(s.Worker.LastFailureCode) {
		return false
	}
	for _, count := range []int{s.Queue.Queued, s.Queue.Running, s.Queue.ExpiredLeases, s.Queue.Completed, s.Queue.Failed, s.Queue.Cancelled, s.Queue.Unknown} {
		if count < 0 || count > MaxRecords {
			return false
		}
	}
	return s.Queue.ExpiredLeases <= s.Queue.Running && s.Queue.Throttled >= 0 && s.Queue.Throttled <= s.Queue.Queued
}

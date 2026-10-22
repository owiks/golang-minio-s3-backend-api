package operations

import (
	"sync"
	"time"
)

// Monitor owns process-local observations, not leases or authorization. A fresh
// process starts without historical success/error timestamps. Store-wide queue
// counts are obtained independently from SummaryReader.
type Monitor struct {
	mu     sync.Mutex
	status WorkerStatus
}

type WorkerStatus struct {
	Scope                string     `json:"scope"`
	Running              bool       `json:"running"`
	Slots                int        `json:"slots"`
	Active               int        `json:"active"`
	StartedAt            *time.Time `json:"startedAt,omitempty"`
	StoppedAt            *time.Time `json:"stoppedAt,omitempty"`
	LastPollAt           *time.Time `json:"lastPollAt,omitempty"`
	LastProgressAt       *time.Time `json:"lastProgressAt,omitempty"`
	LastSuccessfulStepAt *time.Time `json:"lastSuccessfulStepAt,omitempty"`
	LastCompletedAt      *time.Time `json:"lastCompletedAt,omitempty"`
	LastFailureAt        *time.Time `json:"lastFailureAt,omitempty"`
	LastFailureCode      string     `json:"lastFailureCode,omitempty"`
}

func NewMonitor() *Monitor { return &Monitor{status: WorkerStatus{Scope: "process"}} }

func timePointer(value time.Time) *time.Time { value = normalized(value); return &value }

func (m *Monitor) Started(slots int, now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.Running, m.status.Slots = true, slots
	m.status.StartedAt, m.status.StoppedAt = timePointer(now), nil
}

// Stopped is called after joining execution and heartbeat goroutines.
func (m *Monitor) Stopped(now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.Running, m.status.Active = false, 0
	m.status.StoppedAt = timePointer(now)
}

func (m *Monitor) Polled(now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.LastPollAt = timePointer(now)
}

// BeginStep counts admitted execution only, excluding polling and time waiting
// for transfer admission. The returned release may safely be called once.
func (m *Monitor) BeginStep() func() {
	if m == nil {
		return func() {}
	}
	m.mu.Lock()
	m.status.Active++
	m.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.status.Active > 0 {
				m.status.Active--
			}
		})
	}
}

// Checkpoint records completed durable transitions. A nil RunOnce error alone
// is insufficient: failures and unknown outcomes can also be saved correctly.
func (m *Monitor) Checkpoint(saved Snapshot, checkpoint Checkpoint, now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.LastProgressAt = timePointer(now)
	success := checkpoint.Page != nil
	if result := checkpoint.Transfer; result != nil {
		success = false
		if saved.Kind == "transfer" && len(saved.Items) > 0 {
			index := saved.Items[len(saved.Items)-1].Index
			if index >= 0 && index < len(saved.Spec.Transfers) {
				success = result.Copied && (!saved.Spec.Transfers[index].Move || result.Moved) && !result.Uncertain
			}
		}
	}
	if success {
		m.status.LastSuccessfulStepAt = timePointer(now)
		if saved.State == Completed {
			m.status.LastCompletedAt = timePointer(now)
		}
	}
	code := saved.Code
	if result := checkpoint.Transfer; result != nil && !success {
		code = result.Code
	}
	if code != "" && safeCode(code) {
		m.status.LastFailureAt, m.status.LastFailureCode = timePointer(now), code
	}
}

func (m *Monitor) Failure(code string, now time.Time) {
	if m == nil || code == "" || !safeCode(code) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.LastFailureAt, m.status.LastFailureCode = timePointer(now), code
}

func (m *Monitor) Snapshot() WorkerStatus {
	if m == nil {
		return WorkerStatus{Scope: "process"}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v := m.status
	for _, p := range []**time.Time{&v.StartedAt, &v.StoppedAt, &v.LastPollAt, &v.LastProgressAt, &v.LastSuccessfulStepAt, &v.LastCompletedAt, &v.LastFailureAt} {
		if *p != nil {
			*p = timePointer(**p)
		}
	}
	return v
}

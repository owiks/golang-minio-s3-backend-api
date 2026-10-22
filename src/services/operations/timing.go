package operations

import "time"

// Timing measures admitted execution separately from waiting between steps.
// ExecutionSince is a lease-bounded wall clock interval, not CPU or upstream
// service time. A lost worker can account for at most the remaining lease.
type Timing struct {
	StartedAt      time.Time `json:"startedAt,omitempty"`
	FinishedAt     time.Time `json:"finishedAt,omitempty"`
	WaitingSince   time.Time `json:"waitingSince,omitempty"`
	ExecutionSince time.Time `json:"executionSince,omitempty"`
	WaitingUS      int64     `json:"waitingUS"`
	ExecutionUS    int64     `json:"executionUS"`
}

// Durations returns cumulative measured wall-clock durations. Terminal gaps
// before an explicit retry are excluded; queued time across restarts is kept.
func (s Snapshot) Durations(now time.Time) (waiting, execution time.Duration, known bool) {
	if s.Timing == nil {
		return 0, 0, false
	}
	copy := s.Clone()
	finishTiming(&copy, normalized(now))
	return time.Duration(copy.Timing.WaitingUS) * time.Microsecond, time.Duration(copy.Timing.ExecutionUS) * time.Microsecond, true
}

// Phase exposes only coarse stages that the worker can actually establish.
// Moving includes both copying and removing the source; those are one fenced
// executor call, so the API does not pretend to know its internal substage.
func (s Snapshot) Phase() (phase, waitingReason string) {
	if s.Terminal() {
		return s.State, ""
	}
	if s.CancelRequested {
		return "cancelling", ""
	}
	if s.State == Queued {
		return "waiting", "scheduler"
	}
	if s.Timing != nil && s.Timing.ExecutionSince.IsZero() {
		return "waiting", "admission"
	}
	if s.Kind == "scan" {
		return "scanning", ""
	}
	if !s.InFlight {
		return "preparing", ""
	}
	if s.NextIndex < len(s.Spec.Transfers) && s.Spec.Transfers[s.NextIndex].Move {
		return "moving", ""
	}
	return "copying", ""
}

func startTiming(s *Snapshot, now time.Time) {
	t := s.Timing
	if t == nil || !t.ExecutionSince.IsZero() {
		return
	}
	t.WaitingUS += intervalUS(t.WaitingSince, now)
	t.WaitingSince = time.Time{}
	t.ExecutionSince = now
	if t.StartedAt.IsZero() {
		t.StartedAt = now
	}
}

func stopExecution(s *Snapshot, now time.Time) {
	t := s.Timing
	if t == nil || t.ExecutionSince.IsZero() {
		return
	}
	end := now
	if !s.LeaseUntil.IsZero() && s.LeaseUntil.Before(end) {
		end = s.LeaseUntil
	}
	t.ExecutionUS += intervalUS(t.ExecutionSince, end)
	// After lease expiry the old worker has no authority to execute. Keep the
	// recovery delay as waiting, rather than inflating execution duration.
	t.WaitingUS += intervalUS(end, now)
	t.ExecutionSince = time.Time{}
}

func queueTiming(s *Snapshot, now time.Time) {
	if s.Timing == nil {
		return
	}
	stopExecution(s, now)
	if s.Timing.WaitingSince.IsZero() {
		s.Timing.WaitingSince = now
	}
}

func finishTiming(s *Snapshot, now time.Time) {
	if s.Timing == nil || s.Terminal() {
		return
	}
	stopExecution(s, now)
	s.Timing.WaitingUS += intervalUS(s.Timing.WaitingSince, now)
	s.Timing.WaitingSince = time.Time{}
	s.Timing.FinishedAt = now
}

func intervalUS(start, end time.Time) int64 {
	if start.IsZero() || !end.After(start) {
		return 0
	}
	return end.Sub(start).Microseconds()
}

func validTiming(s Snapshot) bool {
	t := s.Timing
	if t == nil {
		return true
	}
	maxUS := s.UpdatedAt.Sub(s.CreatedAt).Microseconds()
	if t.WaitingUS < 0 || t.ExecutionUS < 0 || t.WaitingUS > maxUS || t.ExecutionUS > maxUS-t.WaitingUS {
		return false
	}
	if t.StartedAt.IsZero() && (t.ExecutionUS != 0 || !t.ExecutionSince.IsZero()) {
		return false
	}
	for _, stamp := range []time.Time{t.StartedAt, t.FinishedAt, t.WaitingSince, t.ExecutionSince} {
		if !stamp.IsZero() && (stamp.Before(s.CreatedAt) || stamp.After(s.UpdatedAt)) {
			return false
		}
	}
	if s.Terminal() {
		return t.WaitingSince.IsZero() && t.ExecutionSince.IsZero() && t.FinishedAt.Equal(s.UpdatedAt)
	}
	if !t.FinishedAt.IsZero() || t.WaitingSince.IsZero() == t.ExecutionSince.IsZero() {
		return false
	}
	if s.InFlight && t.ExecutionSince.IsZero() {
		return false
	}
	return t.ExecutionSince.IsZero() || s.State == Running && !t.StartedAt.IsZero() && !t.ExecutionSince.Before(t.StartedAt)
}

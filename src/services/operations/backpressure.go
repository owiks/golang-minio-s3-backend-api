package operations

import (
	"context"
	"time"
)

// CooldownReader returns the connection-wide admission fence without exposing
// the operation that caused it. The fence is also respected by queued transfers.
type CooldownReader interface {
	Cooldown(context.Context, string, time.Time) (time.Time, error)
}

func validBackpressure(s Snapshot) bool {
	if s.ThrottleConsecutive < 0 || s.ThrottleConsecutive > MaxScanThrottleRetries || s.ThrottleTotal < s.ThrottleConsecutive || s.ThrottleTotal > MaxScanThrottleDeferrals {
		return false
	}
	if s.Kind != "scan" && (s.ThrottleConsecutive != 0 || s.ThrottleTotal != 0) {
		return false
	}
	return s.NotBefore.IsZero() || !s.NotBefore.Before(s.CreatedAt) && !s.NotBefore.After(s.UpdatedAt.Add(MaxThrottleDelay))
}

func throttleRecord(s Snapshot, now time.Time) (Snapshot, error) {
	if s.Kind != "scan" || s.InFlight {
		return Snapshot{}, ErrInvalid
	}
	exhausted := s.ThrottleConsecutive >= MaxScanThrottleRetries || s.ThrottleTotal >= MaxScanThrottleDeferrals
	// Observing upstream backpressure still matters when this job is cancelled
	// or exhausts its retry budget. Keep the shared fence for sibling jobs.
	delay := ScanThrottleDelay(min(s.ThrottleConsecutive+1, MaxScanThrottleRetries))
	s.NotBefore = normalized(now.Add(delay))
	if !s.Deadline.IsZero() && now.Before(s.Deadline) && s.Deadline.Before(s.NotBefore) {
		s.NotBefore = s.Deadline
	}
	if s.CancelRequested {
		terminal(&s, Cancelled, "", now)
		return s, nil
	}
	if !authorizationValid(s.Deadline, now) {
		terminal(&s, Failed, "AuthorizationExpired", now)
		return s, nil
	}
	if exhausted {
		terminal(&s, Failed, "ScanThrottled", now)
		return s, nil
	}
	s.ThrottleConsecutive++
	s.ThrottleTotal++
	yield(&s, now)
	s.Revision++
	return s, nil
}

// WaitForCooldown recovers an expired, read-only execution lease without
// starting upstream work. An interrupted mutation must instead become Unknown.
func WaitForCooldown(s Snapshot, until, now time.Time) (Snapshot, error) {
	now, until = recordTime(s, now), normalized(until)
	if s.State != Running || now.Before(s.LeaseUntil) || s.InFlight || !until.After(now) || until.After(now.Add(MaxThrottleDelay)) {
		return Snapshot{}, ErrInvalid
	}
	s = s.Clone()
	if s.CancelRequested {
		terminal(&s, Cancelled, "", now)
	} else if !authorizationValid(s.Deadline, now) {
		terminal(&s, Failed, "AuthorizationExpired", now)
	} else {
		yield(&s, now)
		s.NotBefore = until
		s.UpdatedAt = now
		s.Revision++
	}
	return s, nil
}

func (m *Memory) Cooldown(ctx context.Context, connection string, now time.Time) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return time.Time{}, err
	}
	var until time.Time
	for _, s := range m.records {
		if s.Scope.ConnectionID == connection && s.NotBefore.After(now) && s.NotBefore.After(until) {
			until = s.NotBefore
		}
	}
	return until, nil
}

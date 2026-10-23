package authstate

import "time"

// ExpiryValid treats the zero deadline of a Bearer credential as no expiry.
// Revocation and ancestry are checked separately from this time comparison.
func ExpiryValid(expiry, now time.Time) bool {
	return expiry.IsZero() || now.Before(expiry)
}

// EarlierExpiry intersects deadlines, with zero representing no time limit.
func EarlierExpiry(a, b time.Time) time.Time {
	if a.IsZero() {
		return b
	}
	if b.IsZero() || a.Before(b) {
		return a
	}
	return b
}

func requestedExpiry(now time.Time, seconds int64) time.Time {
	if seconds == 0 {
		return time.Time{}
	}
	return now.Add(time.Duration(seconds) * time.Second)
}

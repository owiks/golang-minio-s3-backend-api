package operations

import "time"

const (
	MaxScanThrottleRetries   = 8
	MaxScanThrottleDeferrals = 64
	MaxThrottleDelay         = time.Minute
)

type throttledError struct{}

func (throttledError) Error() string { return "storage request throttled" }

// ErrThrottled is detached from the SDK failure and contains no resource,
// endpoint, response body, headers or credential data.
var ErrThrottled error = throttledError{}

// ScanThrottleDelay bounds the delay for an explicit load limit on a safe scan
// page. A zero delay rejects an exhausted or invalid automatic retry budget.
func ScanThrottleDelay(consecutive int) time.Duration {
	if consecutive < 1 || consecutive > MaxScanThrottleRetries {
		return 0
	}
	return min(2*time.Second<<uint(consecutive-1), MaxThrottleDelay)
}

package upstream

import (
	"context"
	"errors"
	"net/http"

	"github.com/minio/minio-go/v7"
)

// IsThrottled recognizes an explicit S3 load limit, not a general upstream
// failure. Callers must still decide whether their operation is safe to repeat.
func IsThrottled(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var value minio.ErrorResponse
	var pointer *minio.ErrorResponse
	switch {
	case errors.As(err, &value):
	case errors.As(err, &pointer) && pointer != nil:
		value = *pointer
	default:
		return false
	}
	// Malformed providers can return conflicting status/code pairs. Explicit
	// authentication failures must never become automatic read retries.
	switch value.Code {
	case "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch", "ExpiredToken", "InvalidToken":
		return false
	}
	if value.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if value.StatusCode != 0 && value.StatusCode != http.StatusServiceUnavailable {
		return false
	}
	return value.Code == "SlowDown" || value.Code == "SlowDownRead"
}

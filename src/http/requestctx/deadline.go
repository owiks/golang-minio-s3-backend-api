package requestctx

import (
	"context"
	"net/http"
	"time"
)

// WithDeadline applies the matched operation's budget and parent cancellation.
// Path and method classification belong exclusively to the operation registry.
func WithDeadline(r *http.Request, long bool) (*http.Request, context.CancelFunc, time.Duration) {
	timeout := 30 * time.Second
	if long {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	return r.WithContext(ctx), cancel, timeout
}

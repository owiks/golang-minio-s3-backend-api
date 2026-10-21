package stream

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/result"
	"minio-web/api/src/services/telemetry"
)

// WriteNDJSON starts only after the controller's preflight checks. Errors from
// run are terminal stream events, never a second HTTP/JSON response. A failed
// write or flush cancels the work and permanently stops further sends.
func WriteNDJSON[T any](parent context.Context, w http.ResponseWriter, run func(context.Context, func(result.StreamEvent[T]) bool) error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer InterruptOnCancel(ctx, w)()
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("X-Accel-Buffering", "no")
	encoder := json.NewEncoder(w)
	controller := http.NewResponseController(w)
	var mu sync.Mutex
	terminal, closed, failed, writeFailed := false, false, false, false
	send := func(event result.StreamEvent[T]) bool {
		mu.Lock()
		defer mu.Unlock()
		if terminal || closed || failed {
			return false
		}
		if ctx.Err() != nil {
			failed = true
			telemetry.Outcome(ctx, "cancelled", "")
			return false
		}
		if event.Error != nil {
			telemetry.Error(ctx, event.Error)
			telemetry.Outcome(ctx, "error", event.Error.Code)
		}
		if encoder.Encode(event) != nil || controller.Flush() != nil {
			failed, writeFailed = true, true
			telemetry.Outcome(ctx, "cancelled", "StreamWriteFailed")
			cancel()
			return false
		}
		terminal = event.Result != nil || event.Error != nil
		return true
	}
	if err := run(ctx, send); err != nil {
		telemetry.Error(ctx, err)
		send(result.StreamEvent[T]{Error: apperror.Public(err)})
	}
	mu.Lock()
	defer mu.Unlock()
	closed = true
	// A producer may report its own outcome after a failed progress callback.
	// Delivery failure must remain visible even when the operation returned nil.
	if failed {
		code := ""
		if writeFailed {
			code = "StreamWriteFailed"
		}
		telemetry.Outcome(ctx, "cancelled", code)
	}
}

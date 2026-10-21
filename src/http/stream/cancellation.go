// Package stream owns HTTP delivery and cancellation of streaming responses.
package stream

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// InterruptOnCancel unblocks a downstream Write/Flush when the operation is
// cancelled. Call the returned cleanup before releasing the operation context;
// it waits for a running callback so no late deadline affects another response.
func InterruptOnCancel(ctx context.Context, w http.ResponseWriter) func() {
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(stopped)
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now())
	})
	var once sync.Once
	return func() {
		once.Do(func() {
			if !stop() {
				<-stopped
			}
		})
	}
}

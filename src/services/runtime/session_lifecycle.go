package runtime

import (
	"context"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

func closingError() error {
	return apperror.New(503, "ServerClosing", messages.RuntimeServerClosing)
}
func expiredSessionError() error {
	return apperror.New(401, "SessionExpired", messages.RuntimeSessionExpired)
}
func (app *State) Close() {
	app.closeOnce.Do(func() {
		app.Multipart.Close()
		app.Sessions.Close()
		app.Archives.Close()
		app.Shares.Close()
		app.Invites.Close()
		app.ZIPs.Close()
		app.Capabilities.Close()
		app.revisions.Close()
	})
	// This owner also retains failed and late closes after the stores let go.
	// Repeated Close retries remaining cleanup without closing active readers.
	_ = app.Temporary.Close()
}
func (app *State) reap(now time.Time) {
	app.Multipart.Reap(now)
	app.Sessions.Reap(now)
	app.Archives.Reap(now)
	app.Shares.Reap(now)
	app.Invites.Reap(now)
	app.ZIPs.Reap(now)
	app.Capabilities.Reap(now)
	app.Temporary.Reap(now)
	app.revisions.Reap(now)
}
func (app *State) Reap(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			app.reap(now)
		}
	}
}

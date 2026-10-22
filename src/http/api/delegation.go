package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

// Public capabilities retain their own random secrets, but are additionally
// bound to the current durable parent and its current effective restrictions.
func authorizeDelegated(app *runtime.State, r *http.Request, match routes.Match) error {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch match.Route.Path {
	case "/api/receive":
		if g := app.Invites.Get(token); g != nil && time.Now().Before(g.Expires()) {
			return app.CheckDelegation(r.Context(), g.Owner(), "objects:write", g.Bucket(), g.Prefix())
		}
	case "/api/shared-folder":
		if g := app.Shares.Get(token); g != nil && time.Now().Before(g.Expires()) {
			return app.CheckDelegation(r.Context(), g.Owner(), "objects:read", g.Bucket(), g.Prefix())
		}
	case "/api/shared-folder/file":
		if ticket, ok := app.Shares.TakeTicket(match.Params["ticket"], false); ok {
			return app.CheckDelegation(r.Context(), ticket.Share.Owner(), "objects:read", ticket.Share.Bucket(), ticket.Key)
		}
	}
	return nil
}

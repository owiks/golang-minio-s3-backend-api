package api

import (
	"net/http"
	"strings"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

// Cached plans and operation snapshots must be fenced as well as native I/O:
// narrowing a token may leave old metadata in this process's runtime stores.
func authorizeResources(app *runtime.State, r *http.Request, match routes.Match, s *runtime.Session) error {
	p := s.AccessPolicy()
	if len(p.Resources) == 0 {
		return nil
	}
	q := r.URL.Query()
	if bucket := q.Get("bucket"); bucket != "" {
		key := q.Get("key")
		if key == "" {
			key = q.Get("prefix")
		}
		if !p.AllowsObject(bucket, key) {
			return authstate.Denied()
		}
	}
	if match.Route.Path == "objects/multipart" && match.Params["operation"] != "" {
		if op := app.Multipart.Get(match.Params["operation"]); op != nil && op.Owner() == s.SessionID() {
			v := op.Snapshot()
			if !p.AllowsObject(v.Bucket, v.Key) {
				return authstate.Denied()
			}
		}
	}
	if strings.HasPrefix(match.Route.Path, "objects/archive") && match.Params["ticket"] != "" {
		if plan := app.Archives.Get(match.Params["ticket"], s.SessionID()); plan != nil {
			for _, item := range plan.Snapshot("").Items {
				if !p.AllowsObject(plan.Bucket(), item.Key) {
					return authstate.Denied()
				}
			}
		}
	}
	if match.Route.Path == "objects/invites" && q.Get("token") != "" {
		if g := app.Invites.Get(q.Get("token")); g != nil && g.Owner() == s.SessionID() && !p.AllowsObject(g.Bucket(), g.Prefix()) {
			return authstate.Denied()
		}
	}
	if match.Route.Path == "objects/folder-shares" && q.Get("token") != "" {
		if g := app.Shares.Get(q.Get("token")); g != nil && g.Owner() == s.SessionID() && !p.AllowsObject(g.Bucket(), g.Prefix()) {
			return authstate.Denied()
		}
	}
	return nil
}

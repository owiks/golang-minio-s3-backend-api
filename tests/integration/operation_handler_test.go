package integration

import (
	"net/http"

	"minio-web/api/src/http/api"
	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/runtime"
)

// Controller-level tests use the same method/path selection as production.
// Authentication and mutation locking remain covered through the full API fixture.
func runHTTPAction(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, path string) error {
	registry, err := routes.NewRegistry(api.Catalog())
	if err != nil {
		return err
	}
	match := registry.Match(r.Method, "/api/sessions/fixture/"+path)
	if !match.Found {
		return apperror.New(404, "NotFound", messages.RouteNotFound)
	}
	if !match.MethodAllowed {
		return apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
	}
	return match.Operation.Handler(app, w, r, s, match.Rest)
}

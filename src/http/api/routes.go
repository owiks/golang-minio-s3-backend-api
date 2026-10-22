package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/httperrors"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/middleware"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/requestctx"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/mutation"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
	validationsvc "github.com/owiks/golang-minio-s3-backend-api/src/services/validation"
)

func ServeHTTP(app *runtimesvc.State, w http.ResponseWriter, r *http.Request) {
	r = middleware.LocalizeRequest(r, app.Config().Language, localization.Default())
	serveOperation(app, w, r, registry.Match(r.Method, r.URL.Path))
}
func serveOperation(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, match routes.Match) {
	// Static documentation assets are deliberately outside the application registry.
	if routes.IsDocumentation(r.URL.Path) {
		documentation().ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if err := executeOperation(app, w, r, match); err != nil {
		telemetry.Error(r.Context(), err)
		httperrors.Respond(w, r, err)
	}
}
func executeOperation(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, match routes.Match) error {
	long := match.MethodAllowed && match.Operation.Long
	r, cancel, timeout := requestctx.WithDeadline(r, long)
	defer cancel()
	var s *runtimesvc.Session
	if !match.Found || !match.Route.Public {
		id := match.Params["sessionID"]
		if id == "" {
			// Unknown paths inside a session still authenticate before reporting 404.
			// This is namespace parsing, not a second table of operations.
			if strings.HasPrefix(r.URL.Path, "/api/sessions/") {
				id, _, _ = strings.Cut(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/")
			}
		}
		if id == "" {
			return apperror.New(404, "NotFound", messages.RouteNotFound)
		}
		var err error
		s, err = middleware.Authenticate(app, r, id)
		if err != nil {
			if apperror.Public(err).Code == "InvalidToken" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="minio-s3-api", error="invalid_token"`)
			}
			return err
		}
		r = r.WithContext(s.RequestContext(r.Context()))
		telemetry.Identify(r.Context(), s.ActorName(), s.InternalHost())
	}
	if !match.Found {
		return apperror.New(404, "NotFound", messages.RouteNotFound)
	}
	if !match.MethodAllowed {
		w.Header().Set("Allow", strings.Join(match.Allow, ", "))
		return apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
	}
	if s != nil {
		p := s.AccessPolicy()
		if strings.HasPrefix(match.Route.Path, "admin/buckets") && match.Params["name"] != "" && !p.AllowsBucket(match.Params["name"]) {
			return authstate.Denied()
		}
		if err := authorizeResources(app, r, match, s); err != nil {
			return err
		}
		for _, scope := range match.Operation.Scopes {
			if !p.Allows(scope) {
				return authstate.Denied()
			}
		}
		if s.AccessGuard().Previous {
			for _, scope := range match.Operation.Scopes {
				switch scope {
				case "access:manage", "iam:manage", "server:manage", "buckets:manage":
					return authstate.Denied()
				}
			}
		}
		if s.AccessGuard().Previous && (p.Allows("access:manage") && (strings.HasPrefix(match.Route.Path, "connections") || match.Route.Path == "tokens" || match.Route.Path == "auth-events") || match.Route.Path == "") {
			return authstate.Denied()
		}
	}
	// The former management dispatcher validated these before any SDK call.
	for _, parameter := range []string{"name", "id"} {
		if name := match.Params[parameter]; name != "" && !validationsvc.ValidName(name) {
			return apperror.New(400, "InvalidName", messages.ResourceNameInvalid)
		}
	}
	if match.Route.Public {
		if err := authorizeDelegated(app, r, match); err != nil {
			return err
		}
	}
	if s != nil && long {
		_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(timeout))
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(timeout))
		release, err := app.AcquireTransfer(s.SessionID())
		if err != nil {
			w.Header().Set("Retry-After", "1")
			return err
		}
		defer release()
	}
	if match.NeedsMutationLock() {
		release, err := app.AcquireMutation(r.Context(), s, match.MutationResource(r.URL.Query()))
		if err != nil {
			return err
		}
		defer release()
	}
	succeeded := false
	if match.Operation.InvalidatesAccess {
		ctx, effects := mutation.WithScope(r.Context())
		r = r.WithContext(ctx)
		// Run before releasing the IAM lock, including on cancellation/panic.
		defer func() {
			if succeeded || effects.Effect() != mutation.Unchanged {
				app.InvalidateAccess(s)
				telemetry.Event(r.Context(), "access revision invalidated")
			}
		}()
	}
	for name, value := range match.Params {
		r.SetPathValue(name, value)
	}
	handler := match.Operation.Handler
	err := handler(app, w, r, s, match.Rest)
	succeeded = err == nil
	return err
}

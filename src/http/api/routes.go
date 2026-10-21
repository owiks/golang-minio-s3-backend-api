package api

import (
	"net/http"
	"strings"
	"time"

	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/http/httperrors"
	"minio-web/api/src/http/middleware"
	"minio-web/api/src/http/requestctx"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/mutation"
	runtimesvc "minio-web/api/src/services/runtime"
	"minio-web/api/src/services/telemetry"
	validationsvc "minio-web/api/src/services/validation"
)

func ServeHTTP(app *runtimesvc.State, w http.ResponseWriter, r *http.Request) {
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
		httperrors.Respond(w, err)
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
			return apperror.New(404, "NotFound", "Маршрут API не найден.")
		}
		var err error
		s, err = middleware.Authenticate(app, r, id)
		if err != nil {
			return err
		}
		telemetry.Identify(r.Context(), s.ActorName(), s.InternalHost())
	}
	if !match.Found {
		return apperror.New(404, "NotFound", "Маршрут API не найден.")
	}
	if !match.MethodAllowed {
		w.Header().Set("Allow", strings.Join(match.Allow, ", "))
		return apperror.New(405, "MethodNotAllowed", "Метод не поддерживается.")
	}
	// The former management dispatcher validated these before any SDK call.
	for _, parameter := range []string{"name", "id"} {
		if name := match.Params[parameter]; name != "" && !validationsvc.ValidName(name) {
			return apperror.New(400, "InvalidName", "Имя должно содержать 1–128 символов без пробелов по краям, запятых и слешей.")
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
	handler := match.Operation.Handler
	err := handler(app, w, r, s, match.Rest)
	succeeded = err == nil
	return err
}

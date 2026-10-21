package api

import (
	"net/http"
	"sync"

	"minio-web/api/src/config"
	"minio-web/api/src/http/api/doc"
	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/http/middleware"
	"minio-web/api/src/services/localization"
	"minio-web/api/src/services/runtime"
	"minio-web/api/src/services/telemetry"
)

var documentation = sync.OnceValue(func() http.Handler { return doc.NewHandler(Catalog()) })

type Handler struct {
	*runtime.State
	Metrics  telemetry.Metrics
	catalog  *localization.Catalog
	language string
}

func New(cfg config.Config) *Handler { return NewHandler(runtime.New(cfg)) }

func NewHandler(state *runtime.State) *Handler {
	return &Handler{State: state, catalog: localization.Default(), language: state.Config().Language}
}

func (handler *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r = middleware.LocalizeRequest(r, handler.language, handler.catalog)
	match := registry.Match(r.Method, r.URL.Path)
	name := match.Name(r.Method)
	if routes.IsDocumentation(r.URL.Path) {
		name = "documentation"
	}
	middleware.Observe(w, r, name, &handler.Metrics, func(w http.ResponseWriter, r *http.Request) { serveOperation(handler.State, w, r, match) })
}

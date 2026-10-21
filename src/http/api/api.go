package api

import (
	"net/http"
	"sync"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/doc"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/middleware"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
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

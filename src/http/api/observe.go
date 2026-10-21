package api

import (
	"net/http"

	"minio-web/api/src/http/api/routes"
)

func operationName(r *http.Request) string {
	if routes.IsDocumentation(r.URL.Path) {
		return "documentation"
	}
	return registry.Match(r.Method, r.URL.Path).Name(r.Method)
}

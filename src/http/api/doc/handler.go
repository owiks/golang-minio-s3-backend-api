package doc

import (
	"embed"
	"encoding/json"
	"net/http"
	"strings"

	"minio-web/api/src/http/api/routes"
)

//go:embed assets/*
var assets embed.FS

func NewHandler(catalog []routes.APIRoute) http.Handler {
	specification, err := json.Marshal(Build(catalog))
	if err != nil {
		panic("cannot build API documentation")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/api/openapi.json" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			if r.Method != "HEAD" {
				_, _ = w.Write(specification)
			}
			return
		}
		if r.URL.Path == "/api/docs" {
			http.Redirect(w, r, "/api/docs/", http.StatusTemporaryRedirect)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/api/docs/")
		if name == "" {
			name = "index.html"
		}
		contentType := map[string]string{"index.html": "text/html; charset=utf-8", "swagger-ui-bundle.js": "text/javascript; charset=utf-8", "init.js": "text/javascript; charset=utf-8", "swagger-ui.css": "text/css; charset=utf-8", "theme.css": "text/css; charset=utf-8", "LICENSE": "text/plain; charset=utf-8", "NOTICE": "text/plain; charset=utf-8", "swagger-ui-bundle.js.LICENSE.txt": "text/plain; charset=utf-8"}[name]
		if contentType == "" {
			http.NotFound(w, r)
			return
		}
		value, err := assets.ReadFile("assets/" + name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		if r.Method != "HEAD" {
			_, _ = w.Write(value)
		}
	})
}

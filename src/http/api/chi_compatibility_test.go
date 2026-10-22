package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

// This test-only reference is the dispatcher before chi was introduced. It keeps
// wire compatibility independent of chi's routing defaults and future upgrades.
func legacyRegistryMatcher(catalog []routes.APIRoute) func(method, path string) routes.Match {
	type legacyEntry struct {
		route     routes.APIRoute
		operation routes.Operation
		template  string
		parts     []string
		literals  int
	}
	var entries []legacyEntry
	for _, route := range catalog {
		for _, op := range route.Operations {
			e := legacyEntry{route: route, operation: op, template: route.Template(op)}
			e.parts = strings.Split(strings.TrimPrefix(e.template, "/"), "/")
			for _, p := range e.parts {
				if !strings.HasPrefix(p, "{") {
					e.literals++
				}
			}
			entries = append(entries, e)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].literals > entries[j].literals })
	return func(method, path string) routes.Match {
		if strings.HasPrefix(path, "/api/sessions/") && strings.Count(path, "/") == 4 && strings.HasSuffix(path, "/") {
			path = strings.TrimSuffix(path, "/")
		}
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		var result routes.Match
		specificity := -1
		for _, e := range entries {
			if specificity >= 0 && e.literals < specificity {
				break
			}
			if len(parts) != len(e.parts) {
				continue
			}
			matches := true
			for i, p := range e.parts {
				if parts[i] == "" || p != parts[i] && !strings.HasPrefix(p, "{") {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
			if !result.Found {
				specificity = e.literals
				params := map[string]string{}
				for i, p := range e.parts {
					if strings.HasPrefix(p, "{") {
						params[p[1:len(p)-1]] = parts[i]
					}
				}
				result = routes.Match{Route: e.route, Operation: e.operation, Template: e.template, Params: params, Found: true}
			}
			result.Allow = append(result.Allow, e.operation.Method)
			if e.operation.Method == method {
				result.MethodAllowed = true
				result.Operation = e.operation
				result.Route = e.route
			}
		}
		if suffix := result.Operation.Suffix; suffix != "" {
			result.Rest = strings.Join(parts[len(parts)-len(strings.Split(suffix, "/")):], "/")
		}
		sort.Strings(result.Allow)
		result.Route.Operations = nil
		return result
	}
}

func TestChiMatchesLegacyCatalog(t *testing.T) {
	catalog := Catalog()
	legacy := legacyRegistryMatcher(catalog)
	paths := map[string]bool{"": true, "/": true, "/api/sessions/": true, "/api/sessions//": true, "/unmatched": true}
	for _, route := range catalog {
		for _, op := range route.Operations {
			template := route.Template(op)
			parts := strings.Split(strings.TrimPrefix(template, "/"), "/")
			for i, part := range parts {
				if strings.HasPrefix(part, "{") {
					parts[i] = "fixture"
				}
			}
			path := "/" + strings.Join(parts, "/")
			paths[path] = true
			paths[strings.TrimPrefix(path, "/")] = true
			paths[path+"/"] = true
			paths[path+"//"] = true
			paths[path+"/extra"] = true
			paths["/"+path] = true
			for i, part := range parts {
				modified := append([]string(nil), parts...)
				for _, value := range []string{"", ".", "..", "%2F", "%252F", "a%2fb", "a b", "a;b", "a:b", "a.b", "a*b", "имя", "?", "#", "\\", "a\x00b"} {
					modified[i] = value
					paths["/"+strings.Join(modified, "/")] = true
				}
				modified[i] = part + "/extra"
				paths["/"+strings.Join(modified, "/")] = true
			}
		}
	}
	methods := []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE", "get", "", "OTHER"}
	for path := range paths {
		for _, method := range methods {
			want := legacy(method, path)
			got := registry.Match(method, path)
			wantHandler := reflect.ValueOf(want.Operation.Handler)
			gotHandler := reflect.ValueOf(got.Operation.Handler)
			if wantHandler.Pointer() != gotHandler.Pointer() {
				t.Fatalf("controller changed: %q %q", method, path)
			}
			want.Operation.Handler, got.Operation.Handler = nil, nil
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("route changed: %q %q\n got: %+v\nwant: %+v", method, path, got, want)
			}
		}
	}
	t.Logf("compared %d paths with %d HTTP methods", len(paths), len(methods))
}

func TestChiPreservesDecodedHTTPPathsWithoutRedirects(t *testing.T) {
	app := runtime.New(config.Default())
	defer app.Close()
	handler := NewHandler(app)
	legacy := legacyRegistryMatcher(Catalog())
	for _, target := range []string{
		"/health/", "/health//", "/health/../health", "/%68ealth", "/health%2Fextra",
		"/api/sessions/missing%2Fextra/objects/info", "/api/sessions/missing//objects/info",
		"/api/sessions/missing/objects/../info", "/api/sessions/missing/objects/%69nfo",
	} {
		u, err := url.ParseRequestURI(target)
		if err != nil {
			t.Fatal(err)
		}
		want := legacy(http.MethodGet, u.Path)
		status := http.StatusNotFound
		if strings.HasPrefix(u.Path, "/api/sessions/") {
			status = http.StatusUnauthorized
		} else if want.Found && want.MethodAllowed {
			status = http.StatusOK
		}
		r := httptest.NewRequest(http.MethodGet, target, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status || w.Header().Get("Location") != "" {
			t.Errorf("%s changed decoded path handling: status=%d location=%q", target, w.Code, w.Header().Get("Location"))
		}
	}
}

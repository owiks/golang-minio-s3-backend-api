package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"minio-web/api/src/config"
	"minio-web/api/src/http/api/doc"
	"minio-web/api/src/http/api/openapi"
)

func TestDocumentationMatchesCatalogAndGeneratedFile(t *testing.T) {
	document := doc.Build(Catalog())
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "swagger.generated.json"))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(raw), canonical) {
		t.Fatal("generated Swagger is stale; run make docs")
	}
	for _, path := range []string{"/api/sessions/{sessionID}/objects/upload", "/api/sessions/{sessionID}/admin/users", "/api/sessions/{sessionID}/admin/buckets/{name}/lifecycle"} {
		if document.Paths[path] == nil {
			t.Errorf("missing operation: %s", path)
		}
	}
	login := document.Paths["/api/sessions"]["post"].RequestBody.(map[string]any)
	schema := login["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(openapi.Schema)
	properties := schema["properties"].(map[string]any)
	if properties["secretKey"].(openapi.Schema)["writeOnly"] != true {
		t.Fatal("credentials are not marked writeOnly")
	}

}

func TestDocumentationAssetsAndIsolation(t *testing.T) {
	handler := New(config.Default())
	defer handler.Close()
	for path, want := range map[string]int{"/api/openapi.json": 200, "/api/docs/": 200, "/api/docs/init.js": 200, "/api/docs/swagger-ui-bundle.js": 200, "/api/docs/LICENSE": 200, "/api/docs/../src/main.go": 404, "/api/docs/swagger-ui-bundle.js.map": 404} {
		request := httptest.NewRequest("GET", path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != want {
			t.Errorf("%s: %d", path, response.Code)
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("documentation can be cached: %s", path)
		}
	}
}

func TestIAMDocumentationMatchesEachAction(t *testing.T) {
	document := doc.Build(Catalog())
	for _, tc := range []struct {
		path, method     string
		required, absent []string
	}{
		{"/api/sessions/{sessionID}/admin/users", "post", []string{"name", "secretKey", "acknowledgeNonAtomicCreate"}, []string{"enabled", "confirmation"}},
		{"/api/sessions/{sessionID}/admin/users/{name}/status", "put", []string{"enabled"}, []string{"secretKey", "confirmation"}},
		{"/api/sessions/{sessionID}/admin/users/{name}", "delete", []string{"confirmation"}, []string{"secretKey", "enabled"}},
		{"/api/sessions/{sessionID}/admin/policies", "post", []string{"name", "document", "acknowledgeNonAtomicCreate"}, nil},
		{"/api/sessions/{sessionID}/admin/groups", "post", []string{"name", "members", "acknowledgeNonAtomicCreate"}, nil},
		{"/api/sessions/{sessionID}/admin/policies/{name}", "put", []string{"document", "revision"}, nil},
		{"/api/sessions/{sessionID}/admin/access-keys/{name}/rotate", "post", []string{"confirmation", "revision"}, []string{"enabled", "policy"}},
	} {
		operation := document.Paths[tc.path][tc.method]
		raw, err := json.Marshal(operation.RequestBody)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Content map[string]struct {
				Schema struct {
					Properties map[string]any
					Required   []string
					AnyOf      []any
				}
			}
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		schema := body.Content["application/json"].Schema
		if len(schema.AnyOf) > 0 {
			t.Fatal("action shows unrelated DTOs", tc.path)
		}
		for _, field := range tc.required {
			found := false
			for _, r := range schema.Required {
				if r == field {
					found = true
				}
			}
			if !found {
				t.Errorf("%s missing required %s", tc.path, field)
			}
		}
		for _, field := range tc.absent {
			if _, ok := schema.Properties[field]; ok {
				t.Errorf("%s exposes unrelated %s", tc.path, field)
			}
		}
	}
}

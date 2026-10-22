package api

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/doc"
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
		if document.Paths.Value(path) == nil {
			t.Errorf("missing operation: %s", path)
		}
	}
	login := document.Paths.Value("/api/sessions").GetOperation("POST").RequestBody.Value
	schema := login.Content["application/json"].Schema.Value
	properties := schema.Properties
	if !properties["secretKey"].Value.WriteOnly {
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
		operation := document.Paths.Value(tc.path).GetOperation(strings.ToUpper(tc.method))
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

func TestLanguageHeadersIncludeGeneratedArchivesAndPreserveRawBinaryContract(t *testing.T) {
	document := doc.Build(Catalog())
	prefix := "/api/sessions/{sessionID}/"
	for _, tc := range []struct {
		path, media string
		localized   bool
	}{
		{"objects/archive/{ticket}", "application/zip", true},
		{"objects/preview", "text/plain", false},
	} {
		response := document.Paths.Value(prefix + tc.path).GetOperation("GET").Responses.Value("200").Value
		headers := response.Headers
		if (headers["Content-Language"] != nil) != tc.localized || (headers["Vary"] != nil) != tc.localized || headers["X-Request-ID"] == nil {
			t.Fatal(tc.path, headers)
		}
		content := response.Content
		schema := content[tc.media].Schema.Value
		if !schema.Type.Is("string") || schema.Format != "binary" {
			t.Fatal("binary contract changed", schema)
		}
	}
}

func TestBearerDocumentationPreservesCookieAndGrantSecurity(t *testing.T) {
	document := doc.Build(Catalog())
	for _, route := range Catalog() {
		if route.Public {
			continue
		}
		for _, op := range route.Operations {
			operation := document.Paths.Value(route.Template(op)).GetOperation(op.Method)
			if len(*operation.Security) != 2 {
				t.Fatal("missing alternative authentication", route.Template(op))
			}
			if _, ok := (*operation.Security)[0]["SessionCookie"]; !ok {
				t.Fatal("cookie authentication removed")
			}
			if _, ok := (*operation.Security)[1]["SessionBearer"]; !ok || len((*operation.Security)[1]) != 1 {
				t.Fatal("bearer incorrectly requires cookie or CSRF")
			}
			_, csrf := (*operation.Security)[0]["CSRF"]
			if csrf != (op.Method != "GET") {
				t.Fatal("cookie CSRF contract changed")
			}
			for _, ref := range operation.Parameters {
				p := ref.Value
				if p.In == "header" && p.Name == "Origin" && p.Required {
					t.Fatal("Origin is mandatory for bearer")
				}
			}
		}
	}
	issue := document.Paths.Value("/api/tokens").GetOperation("POST")
	if len(*issue.Security) != 0 {
		t.Fatal("issuance requires an existing token")
	}
	schema := issue.RequestBody.Value.Content["application/json"].Schema.Value
	properties := schema.Properties
	if !properties["secretKey"].Value.WriteOnly || properties["ttlSeconds"] == nil {
		t.Fatal("invalid token request documentation")
	}
	for _, methods := range document.Paths.Map() {
		for _, operation := range methods.Operations() {
			for _, requirement := range *operation.Security {
				if _, grant := requirement["BearerGrant"]; grant && len(*operation.Security) != 1 {
					t.Fatal("public grant now accepts a session bearer")
				}
			}
		}
	}
}

func TestStandaloneOpenAPIDocumentPassesLibraryValidation(t *testing.T) {
	raw, err := json.Marshal(doc.Build(Catalog()))
	if err != nil {
		t.Fatal(err)
	}
	document, err := openapi3.NewLoader().LoadFromData(raw)
	if err != nil {
		t.Fatal("unresolved OpenAPI references", err)
	}
	if err := document.Validate(context.Background()); err != nil {
		t.Fatal("invalid OpenAPI specification", err)
	}
	// kin-openapi represents JSON Schema numeric bounds as float64. In
	// particular MaxInt64 is rounded in JSON; native API parsing still uses int64.
	search := document.Paths.Value("/api/sessions/{sessionID}/objects/search").Get
	for _, name := range []string{"minSize", "maxSize"} {
		bound := inputParameter(t, search, name).Schema.Value
		if !bound.Type.Is("integer") || bound.Min == nil || *bound.Min != 0 || bound.Max == nil || *bound.Max != float64(math.MaxInt64) {
			t.Fatalf("numeric parameter contract changed: %s: %+v", name, bound)
		}
	}
}

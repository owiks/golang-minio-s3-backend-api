package api

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"minio-web/api/src/http/api/doc"
	"minio-web/api/src/http/api/openapi"
	servercontroller "minio-web/api/src/http/api/routes/server/controllers"
	uploadcontroller "minio-web/api/src/http/api/routes/uploads/controllers"
	"minio-web/api/src/services/apperror"
)

func TestInputContractsMatchValidationFailures(t *testing.T) {
	document := doc.Build(Catalog())
	// These handlers reject input before accessing clients, so no MinIO or credentials are used.
	for _, tc := range []struct {
		name, path, method, parameter string
		minimum, maximum              int64
		run                           func(string) error
		invalid                       []string
		code                          string
	}{
		{"diagnostics seconds", "admin/diagnostics", "get", "seconds", 1, 10, func(value string) error {
			return servercontroller.HandleDiagnostics(nil, httptest.NewRecorder(), httptest.NewRequest("GET", "/?kind=trace&level=ALL&limit=1&seconds="+value, nil), nil)
		}, []string{"", "0", "11", "1.5"}, "InvalidDiagnostic"},
		{"diagnostics limit", "admin/diagnostics", "get", "limit", 1, 200, func(value string) error {
			return servercontroller.HandleDiagnostics(nil, httptest.NewRecorder(), httptest.NewRequest("GET", "/?kind=trace&level=ALL&seconds=1&limit="+value, nil), nil)
		}, []string{"", "0", "201", "many"}, "InvalidDiagnostic"},
		{"upload size", "objects/upload", "put", "X-Upload-Size", 0, 5368709120, func(value string) error {
			r := httptest.NewRequest("PUT", "/?bucket=audit-bucket&key=test.txt", strings.NewReader("abc"))
			r.Header.Set("X-Upload-Size", value)
			return uploadcontroller.HandleWriteFile(nil, httptest.NewRecorder(), r, nil)
		}, []string{"", "-1", "5368709121", "abc"}, "InvalidUploadSize"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			operation := document.Paths["/api/sessions/{sessionID}/"+tc.path][tc.method]
			p := inputParameter(t, operation, tc.parameter)
			if !p.Required || p.Schema["type"] != "integer" || p.Schema["minimum"] != tc.minimum || p.Schema["maximum"] != tc.maximum {
				t.Fatalf("wrong parameter contract: %+v", p)
			}
			for _, value := range tc.invalid {
				err := tc.run(value)
				if err == nil {
					t.Fatalf("invalid value accepted: %q", value)
				}
				problem := apperror.Public(err)
				if problem.Status != 400 || problem.Code != tc.code {
					t.Fatalf("%q: %+v", value, problem)
				}
			}
		})
	}
}

func inputParameter(t *testing.T, operation openapi.Operation, name string) openapi.Parameter {
	t.Helper()
	for _, p := range operation.Parameters {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("missing parameter: %s", name)
	return openapi.Parameter{}
}

func TestInputsAreSpecificToEachAction(t *testing.T) {
	document := doc.Build(Catalog())
	prefix := "/api/sessions/{sessionID}/"
	for _, tc := range []struct {
		path, method   string
		query, headers []string
		body           bool
	}{
		{"buckets", "get", nil, nil, false},
		{"admin/buckets", "get", nil, nil, false},
		{"admin/buckets", "post", nil, []string{"Origin"}, true},
		{"admin/buckets/{name}", "delete", nil, []string{"Origin"}, true},
		{"admin/buckets/{name}/details", "get", []string{"fields"}, nil, false},
		{"objects", "get", []string{"bucket", "prefix", "token"}, nil, false},
		{"objects/prepare", "get", []string{"bucket", "key"}, nil, false},
		{"objects/upload", "put", []string{"bucket", "key"}, []string{"Origin", "X-Object-Revision", "X-Upload-Checksum-Algorithm", "X-Upload-Checksum-Encoding", "X-Upload-Checksum-Type", "X-Upload-Checksum-Value", "X-Upload-Size"}, true},
		{"capabilities", "get", []string{"bucket", "feature", "key", "versionId"}, nil, false},
		{"objects/attributes", "get", []string{"bucket", "key", "maxParts", "partNumberMarker", "snapshot", "versionId"}, nil, false},
		{"objects/select", "post", []string{"bucket", "key"}, []string{"Origin"}, true},
		{"objects/multipart", "post", nil, []string{"Origin"}, true},
		{"objects/multipart/{operation}/parts/{part}", "put", nil, []string{"Origin", "X-Part-Checksum-SHA256", "X-Upload-Size"}, true},
		{"objects/multipart/{operation}/parts/{part}/sign", "post", nil, []string{"Origin"}, true},
		{"admin/access-keys/{name}", "get", nil, nil, false},
		{"admin/incomplete-uploads/parts", "get", []string{"bucket", "key", "summary", "uploadId"}, nil, false},
		{"admin/incomplete-uploads/abort", "post", nil, []string{"Origin"}, true},
		{"objects/invites", "delete", []string{"token"}, []string{"Origin"}, false},
		{"objects/folder-shares", "delete", []string{"token"}, []string{"Origin"}, false},
		{"objects/archive/{ticket}", "delete", nil, []string{"Origin"}, false},
		{"objects/archive-status/{ticket}", "delete", nil, []string{"Origin"}, false},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			operation := document.Paths[prefix+tc.path][tc.method]
			var query, headers []string
			for _, p := range operation.Parameters {
				if p.In == "query" {
					query = append(query, p.Name)
				}
				if p.In == "header" {
					headers = append(headers, p.Name)
				}
			}
			slices.Sort(query)
			slices.Sort(headers)
			if !slices.Equal(query, tc.query) || !slices.Equal(headers, tc.headers) || (operation.RequestBody != nil) != tc.body {
				t.Fatalf("mixed action inputs: query=%v headers=%v body=%t", query, headers, operation.RequestBody != nil)
			}
			if tc.body && operation.RequestBody.(map[string]any)["required"] != true {
				t.Fatal("decoded body is optional")
			}
		})
	}
}

func TestSearchFiltersAndPublicGrantSecurityAreDocumented(t *testing.T) {
	document := doc.Build(Catalog())
	search := document.Paths["/api/sessions/{sessionID}/objects/search"]["get"]
	for _, name := range []string{"minSize", "maxSize"} {
		p := inputParameter(t, search, name)
		if p.Required || p.Schema["type"] != "integer" || p.Schema["minimum"] != int64(0) {
			t.Fatal(p)
		}
	}
	for _, name := range []string{"after", "before"} {
		if inputParameter(t, search, name).Schema["format"] != "date" {
			t.Fatal(name)
		}
	}
	for _, path := range []string{"/api/receive", "/api/shared-folder"} {
		for _, operation := range document.Paths[path] {
			if !reflect.DeepEqual(operation.Security, []map[string][]string{{"BearerGrant": {}}}) {
				t.Fatal("missing bearer security", path)
			}
			for _, p := range operation.Parameters {
				if strings.EqualFold(p.Name, "Authorization") {
					t.Fatal("OpenAPI ignores Authorization header parameters; use security", path)
				}
			}
		}
	}
	if inputParameter(t, document.Paths["/api/receive"]["put"], "X-Upload-Size").Schema["maximum"] != int64(1073741824) {
		t.Fatal("wrong invitation upload limit")
	}
	scope := inputParameter(t, document.Paths["/api/sessions/{sessionID}/admin/access-keys"]["get"], "scope")
	if !slices.Equal(scope.Schema["enum"].([]string), []string{"own", "user", "all"}) {
		t.Fatal(scope)
	}
}

func TestRequestBodiesAreExplicitAndRequired(t *testing.T) {
	document := doc.Build(Catalog())
	prefix := "/api/sessions/{sessionID}/"
	for _, tc := range []struct {
		path, method string
		required     []string
	}{
		{"admin/batch-jobs", "post", []string{"type", "bucket", "confirmation"}},
		{"admin/batch-jobs/{id}", "delete", []string{"confirmation"}},
		{"admin/server-config", "put", []string{"revision"}},
		{"admin/buckets/{name}/lifecycle", "patch", []string{"revision", "index", "enabled"}},
		{"objects/content", "put", []string{"revision"}},
	} {
		body := document.Paths[prefix+tc.path][tc.method].RequestBody.(map[string]any)
		raw, _ := json.Marshal(body["content"].(map[string]any)["application/json"].(map[string]any)["schema"])
		var schema struct {
			Required   []string
			Properties map[string]map[string]any
			AnyOf      []any
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		if body["required"] != true || len(schema.AnyOf) != 0 {
			t.Fatal("body missing or mixes unrelated DTOs", tc)
		}
		for _, name := range tc.required {
			if !slices.Contains(schema.Required, name) || schema.Properties[name] == nil || schema.Properties[name]["nullable"] == true {
				t.Fatal("required input lost", tc.path, name)
			}
		}
	}
}

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime"
	"strconv"
	"strings"
	"sync"
	"testing"

	apihttp "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/doc"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
)

// Successful fixture responses are checked against the same explicit models
// exposed to API users. This catches silent shape changes after Go refactoring.
var responseRegistry = sync.OnceValue(func() *routes.Registry {
	registry, err := routes.NewRegistry(apihttp.Catalog())
	if err != nil {
		panic(err)
	}
	return registry
})
var responseDocument = sync.OnceValue(func() map[string]any {
	raw, err := json.Marshal(doc.Build(apihttp.Catalog()))
	if err != nil {
		panic(err)
	}
	var result map[string]any
	if err = json.Unmarshal(raw, &result); err != nil {
		panic(err)
	}
	return result
})

func assertResponseContract(t *testing.T, method, path string, status int, contentType string, data []byte) {
	t.Helper()
	if status < 200 || status >= 300 || method == "HEAD" {
		return
	}
	match := responseRegistry().Match(method, strings.SplitN(path, "?", 2)[0])
	if !match.MethodAllowed {
		t.Fatalf("successful unregistered operation: %s %s", method, path)
	}
	operation := responseDocument()["paths"].(map[string]any)[match.Template].(map[string]any)[strings.ToLower(method)].(map[string]any)
	response, ok := operation["responses"].(map[string]any)[strconv.Itoa(status)].(map[string]any)
	if !ok {
		t.Fatalf("undocumented successful status: %s %s %d", method, match.Template, status)
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("missing response content type: %s %s", method, match.Template)
	}
	content, _ := response["content"].(map[string]any)
	declared, ok := content[mediaType].(map[string]any)
	if !ok {
		for key, value := range content {
			if strings.HasSuffix(key, "/*") && strings.HasPrefix(mediaType, strings.TrimSuffix(key, "*")) {
				declared = value.(map[string]any)
				ok = true
				break
			}
		}
	}
	if !ok {
		t.Fatalf("undocumented media type: %s %s %s", method, match.Template, mediaType)
	}
	if mediaType != "application/json" && mediaType != "application/x-ndjson" {
		return
	}
	schema := declared["schema"].(map[string]any)
	lines := [][]byte{data}
	if mediaType == "application/x-ndjson" {
		lines = bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	}
	for _, line := range lines {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			t.Fatalf("invalid JSON response: %s %s: %v", method, match.Template, err)
		}
		if err := checkWireSchema(schema, value, "$"); err != nil {
			t.Fatalf("response violates explicit model: %s %s %d: %v", method, match.Template, status, err)
		}
	}
}
func checkWireSchema(schema map[string]any, value any, path string) error {
	if value == nil {
		if schema["nullable"] == true || len(schema) == 0 {
			return nil
		}
		return fmt.Errorf("%s: unexpected null", path)
	}
	if variants, ok := schema["anyOf"].([]any); ok {
		for _, variant := range variants {
			if checkWireSchema(variant.(map[string]any), value, path) == nil {
				return nil
			}
		}
		return fmt.Errorf("%s: no response variant matches", path)
	}
	switch schema["type"] {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected object", path)
		}
		properties, _ := schema["properties"].(map[string]any)
		required, _ := schema["required"].([]any)
		for _, name := range required {
			if _, ok := object[name.(string)]; !ok {
				return fmt.Errorf("%s.%s: missing required field", path, name)
			}
		}
		for key, item := range object {
			field, ok := properties[key].(map[string]any)
			if !ok {
				if schema["additionalProperties"] == false {
					return fmt.Errorf("%s.%s: unexpected field", path, key)
				}
				field, _ = schema["additionalProperties"].(map[string]any)
			}
			if field != nil {
				if err := checkWireSchema(field, item, path+"."+key); err != nil {
					return err
				}
			}
		}
	case "array":
		array, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s: expected array", path)
		}
		items, _ := schema["items"].(map[string]any)
		for i, item := range array {
			if err := checkWireSchema(items, item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s: expected string", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s: expected boolean", path)
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok || strings.ContainsAny(number.String(), ".eE") {
			return fmt.Errorf("%s: expected integer", path)
		}
	case "number":
		if _, ok := value.(json.Number); !ok {
			return fmt.Errorf("%s: expected number", path)
		}
	}
	return nil
}
func TestWireContractCheckerRejectsShapeDrift(t *testing.T) {
	schema := map[string]any{"type": "object", "required": []any{"ok"}, "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "additionalProperties": false}
	for _, value := range []any{nil, map[string]any{}, map[string]any{"ok": "true"}, map[string]any{"ok": true, "secret": "fixture"}} {
		if checkWireSchema(schema, value, "$") == nil {
			t.Fatalf("invalid wire shape accepted: %#v", value)
		}
	}
	if err := checkWireSchema(schema, map[string]any{"ok": false}, "$"); err != nil {
		t.Fatal(err)
	}
}

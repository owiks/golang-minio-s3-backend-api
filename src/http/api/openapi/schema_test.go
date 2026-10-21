package openapi

import (
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/identities"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/keys"
)

func TestExplicitContractsPreserveInheritanceAndRequiredPointers(t *testing.T) {
	schema := TypeSchema(keys.AccessKeyDetails{}, false)
	p := schema["properties"].(map[string]any)
	for _, field := range []string{"accessKey", "parent", "status", "policy", "revision"} {
		if p[field] == nil {
			t.Fatal("embedded response field missing", field)
		}
	}
	schema = TypeSchema(identities.StatusRequest{}, true)
	enabled := schema["properties"].(map[string]any)["enabled"].(Schema)
	if enabled["type"] != "boolean" || enabled["nullable"] == true {
		t.Fatal("required pointer must reject null", enabled)
	}
	schema = TypeSchema(identities.CreateUserRequest{}, true)
	secret := schema["properties"].(map[string]any)["secretKey"].(Schema)
	if secret["writeOnly"] != true || secret["x-minBytes"] != 8 {
		t.Fatal(secret)
	}
	response := TypeSchema(keys.GeneratedKey{}, false)["properties"].(map[string]any)["secretKey"].(Schema)
	if response["writeOnly"] == true || response["readOnly"] != true {
		t.Fatal("generated secret is a response, not a request field")
	}
}

func TestResponseRequiredFieldsAreUnique(t *testing.T) {
	schema := TypeSchema(keys.AccessKeyDetails{}, false)
	seen := map[string]bool{}
	for _, name := range schema["required"].([]string) {
		if seen[name] {
			t.Fatal("duplicate required field", name)
		}
		seen[name] = true
	}
}

package openapi

import (
	"encoding/json"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/identities"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/keys"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

func TestExplicitContractsPreserveInheritanceAndRequiredPointers(t *testing.T) {
	schema := TypeSchema(keys.AccessKeyDetails{}, false)
	p := schema.Value.Properties
	for _, field := range []string{"accessKey", "parent", "status", "policy", "revision"} {
		if p[field] == nil {
			t.Fatal("embedded response field missing", field)
		}
	}
	schema = TypeSchema(identities.StatusRequest{}, true)
	enabled := schema.Value.Properties["enabled"].Value
	if !enabled.Type.Is("boolean") || enabled.Nullable {
		t.Fatal("required pointer must reject null", enabled)
	}
	schema = TypeSchema(identities.CreateUserRequest{}, true)
	secret := schema.Value.Properties["secretKey"].Value
	if !secret.WriteOnly || secret.Extensions["x-minBytes"] != uint64(8) {
		t.Fatal(secret)
	}
	response := TypeSchema(keys.GeneratedKey{}, false).Value.Properties["secretKey"].Value
	if response.WriteOnly || !response.ReadOnly {
		t.Fatal("generated secret is a response, not a request field")
	}
}

func TestResponseRequiredFieldsAreUnique(t *testing.T) {
	schema := TypeSchema(keys.AccessKeyDetails{}, false)
	seen := map[string]bool{}
	for _, name := range schema.Value.Required {
		if seen[name] {
			t.Fatal("duplicate required field", name)
		}
		seen[name] = true
	}
}

func TestGeneratedSchemaPreservesWireMessagesAndFieldIsolation(t *testing.T) {
	type model struct {
		Message  messages.Ref      `json:"message"`
		Warnings []messages.Ref    `json:"warnings"`
		Optional *bool             `json:"optional,omitempty"`
		Required *bool             `json:"required" validate:"required"`
		Raw      json.RawMessage   `json:"raw"`
		Values   map[string]string `json:"values"`
		Bounded  string            `json:"bounded" validate:"minBytes=2,maxBytes=12"`
		Plain    string            `json:"plain"`
	}
	for _, request := range []bool{false, true} {
		schema := TypeSchema(model{}, request).Value
		if !schema.Properties["message"].Value.Type.Is("string") {
			t.Fatal("message descriptor exposed instead of localized string")
		}
		if !schema.Properties["warnings"].Value.Nullable || !schema.Properties["warnings"].Value.Items.Value.Type.Is("string") {
			t.Fatal("localized collection contract changed")
		}
		if schema.Properties["required"].Value.Nullable || !schema.Properties["optional"].Value.Nullable {
			t.Fatal("required pointer polluted optional field")
		}
		if schema.Properties["raw"].Value.Type != nil || !schema.Properties["values"].Value.Nullable {
			t.Fatal("raw JSON or map nullability changed")
		}
		bounded := schema.Properties["bounded"].Value
		if bounded.Extensions["x-minBytes"] != uint64(2) || bounded.Extensions["x-maxBytes"] != uint64(12) || bounded.MinLength != 0 || bounded.MaxLength != nil || len(schema.Properties["plain"].Value.Extensions) > 0 {
			t.Fatal("UTF-8 byte limits converted to character limits or leaked to another field")
		}
	}
}

package openapi

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3gen"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

// TypeSchema uses kin-openapi's type graph and JSON field discovery. The policy
// below preserves the API's existing wire contracts; validate tags document
// service validation and never enable HTTP request validation.
func TypeSchema(value any, request bool) *openapi3.SchemaRef {
	if value == nil {
		return &openapi3.SchemaRef{Value: openapi3.NewSchema()}
	}
	g := openapi3gen.NewGenerator(openapi3gen.UseAllExportedFields(), openapi3gen.ThrowErrorOnCycle(), openapi3gen.CreateFieldNameGenerator(jsonFieldName))
	// Messages are localized strings at the HTTP boundary, never their internal
	// descriptors. Preseeding also avoids openapi3gen treating the named Ref
	// interface as one of kin-openapi's struct reference wrappers.
	g.Types[reflect.TypeFor[messages.Ref]()] = &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}
	ref, err := g.NewSchemaRefForValue(value, nil)
	if err != nil {
		panic(fmt.Sprintf("cannot generate API schema for %T: %v", value, err))
	}
	for t, generated := range g.Types {
		applyTypePolicy(t, generated.Value, request)
	}
	// Field constraints require detached schemas: a required field must not
	// change nullability or secrecy of another field with the same Go type.
	for t, generated := range g.Types {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct || t == reflect.TypeFor[time.Time]() {
			continue
		}
		for _, field := range reflect.VisibleFields(t) {
			name := jsonFieldName(field, "")
			property := generated.Value.Properties[name]
			if property == nil {
				continue
			}
			copy := *property.Value
			applyFieldPolicy(name, field.Tag, &copy, request)
			generated.Value.Properties[name] = &openapi3.SchemaRef{Value: &copy}
		}
	}
	return ref
}

func jsonFieldName(field reflect.StructField, _ string) string {
	name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	if name == "" {
		return field.Name
	}
	return name
}

func applyTypePolicy(t reflect.Type, schema *openapi3.Schema, request bool) {
	if t == reflect.TypeFor[json.RawMessage]() || t == reflect.TypeFor[messages.Ref]() {
		return
	}
	if t.Kind() == reflect.Pointer {
		if schema.Type != nil {
			schema.Nullable = true
		}
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
	}
	if schema.Type.Is("integer") {
		schema.Format, schema.Min, schema.Max = "int64", nil, nil
	}
	if schema.Type.Is("number") {
		schema.Format = ""
	}
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
		schema.Nullable = true
	}
	if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
		schema.Type, schema.Format = &openapi3.Types{"array"}, ""
		schema.Items = &openapi3.SchemaRef{Value: openapi3.NewIntegerSchema().WithFormat("int64")}
	}
	if t.Kind() != reflect.Struct || t == reflect.TypeFor[time.Time]() {
		return
	}
	schema.Type = &openapi3.Types{"object"}
	schema.AdditionalProperties = openapi3.AdditionalProperties{Has: new(false)}
	for _, field := range reflect.VisibleFields(t) {
		name := jsonFieldName(field, "")
		if schema.Properties[name] == nil {
			continue
		}
		if slices.Contains(strings.Split(field.Tag.Get("validate"), ","), "required") || (!request && !slices.Contains(strings.Split(field.Tag.Get("json"), ","), "omitempty")) {
			schema.Required = append(schema.Required, name)
		}
	}
	slices.Sort(schema.Required)
	schema.Required = slices.Compact(schema.Required)
}

func applyFieldPolicy(name string, tag reflect.StructTag, schema *openapi3.Schema, request bool) {
	if strings.Contains(strings.ToLower(name), "secret") || name == "sessionToken" {
		schema.WriteOnly, schema.ReadOnly = request, !request
	}
	for _, rule := range strings.Split(tag.Get("validate"), ",") {
		if rule == "required" {
			schema.Nullable = false
		}
		key, value, _ := strings.Cut(rule, "=")
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			continue
		}
		if schema.Type.Is("array") {
			if key == "min" {
				schema.MinItems = n
			}
			if key == "max" {
				schema.MaxItems = &n
			}
		}
		if schema.Type.Is("string") && (key == "minBytes" || key == "maxBytes") {
			if schema.Extensions == nil {
				schema.Extensions = make(map[string]any)
			}
			schema.Extensions["x-"+key] = n
			if key == "minBytes" {
				schema.Description = fmt.Sprintf("Минимум %d байт UTF-8.", n)
			} else {
				schema.Description = strings.TrimSpace(schema.Description + fmt.Sprintf(" Максимум %d байт UTF-8.", n))
			}
		}
	}
}

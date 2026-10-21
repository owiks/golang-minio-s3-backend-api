package openapi

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

// TypeSchema is used for explicit per-operation contracts. Runtime validation
// stays in services; validate tags document only constraints enforced there.
func TypeSchema(value any, request bool) Schema { return typeSchema(reflect.TypeOf(value), request, 0) }
func typeSchema(t reflect.Type, request bool, depth int) Schema {
	if t == nil || depth > 16 {
		return Schema{}
	}
	if t == reflect.TypeFor[messages.Ref]() {
		return Schema{"type": "string"}
	}
	if t == reflect.TypeFor[json.RawMessage]() {
		return Schema{}
	}
	if t == reflect.TypeFor[time.Time]() {
		return Schema{"type": "string", "format": "date-time"}
	}
	if t.Kind() == reflect.Pointer {
		s := typeSchema(t.Elem(), request, depth+1)
		if _, ok := s["type"]; ok {
			s["nullable"] = true
		}
		return s
	}
	switch t.Kind() {
	case reflect.String:
		return Schema{"type": "string"}
	case reflect.Bool:
		return Schema{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return Schema{"type": "integer", "format": "int64"}
	case reflect.Float32, reflect.Float64:
		return Schema{"type": "number"}
	case reflect.Slice:
		return Schema{"type": "array", "nullable": true, "items": typeSchema(t.Elem(), request, depth+1)}
	case reflect.Array:
		return Schema{"type": "array", "items": typeSchema(t.Elem(), request, depth+1)}
	case reflect.Map:
		return Schema{"type": "object", "nullable": true, "additionalProperties": typeSchema(t.Elem(), request, depth+1)}
	case reflect.Struct:
		properties := map[string]any{}
		required := []string{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := f.Tag.Get("json")
			name := strings.Split(tag, ",")[0]
			if name == "-" {
				continue
			}
			if f.Anonymous && name == "" {
				nested := typeSchema(f.Type, request, depth+1)
				if values, ok := nested["properties"].(map[string]any); ok {
					for k, v := range values {
						properties[k] = v
					}
				}
				if values, ok := nested["required"].([]string); ok {
					required = append(required, values...)
				}
				continue
			}
			if name == "" {
				name = f.Name
			}
			s := typeSchema(f.Type, request, depth+1)
			if strings.Contains(strings.ToLower(name), "secret") || name == "sessionToken" {
				if request {
					s["writeOnly"] = true
				} else {
					s["readOnly"] = true
				}
			}
			for _, rule := range strings.Split(f.Tag.Get("validate"), ",") {
				if rule == "required" {
					required = append(required, name)
					delete(s, "nullable")
				}
				if key, value, ok := strings.Cut(rule, "="); ok {
					if n, err := strconv.Atoi(value); err == nil {
						if s["type"] == "array" {
							if key == "min" {
								s["minItems"] = n
							}
							if key == "max" {
								s["maxItems"] = n
							}
						}
						if s["type"] == "string" {
							if key == "minBytes" {
								s["x-minBytes"] = n
								s["description"] = fmt.Sprintf("Минимум %d байт UTF-8.", n)
							}
							if key == "maxBytes" {
								s["x-maxBytes"] = n
								prefix, _ := s["description"].(string)
								s["description"] = strings.TrimSpace(prefix + fmt.Sprintf(" Максимум %d байт UTF-8.", n))
							}
						}
					}
				}
			}
			if !request && !strings.Contains(tag, ",omitempty") {
				required = append(required, name)
			}
			properties[name] = s
		}
		s := Schema{"type": "object", "properties": properties, "additionalProperties": false}
		if len(required) > 0 {
			seen := map[string]bool{}
			unique := []string{}
			for _, name := range required {
				if !seen[name] {
					seen[name] = true
					unique = append(unique, name)
				}
			}
			sort.Strings(unique)
			s["required"] = unique
		}
		return s
	}
	return Schema{}
}

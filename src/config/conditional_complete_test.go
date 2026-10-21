package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestConditionalCompleteDefaultsToUnknownAndRequiresExactConfiguredEndpoint(t *testing.T) {
	c, err := LoadEnv(env(nil))
	if err != nil || len(c.ConditionalCompleteEndpoints) != 0 {
		t.Fatal("conditional completion enabled by default", c, err)
	}
	c, err = LoadEnv(env(map[string]string{"API_CONDITIONAL_COMPLETE_ENDPOINTS": " https://S3.EXAMPLE/, ,http://MINIO:9000/ , https://s3.example"}))
	want := map[string]bool{"https://s3.example": true, "http://minio:9000": true}
	if err != nil || !reflect.DeepEqual(c.ConditionalCompleteEndpoints, want) {
		t.Fatal("endpoint assertions not canonical", c.ConditionalCompleteEndpoints, err)
	}
	if len(c.Endpoints.Allowed) != 0 || len(c.Endpoints.Overrides) != 0 || len(c.ConditionalDeleteEndpoints) != 0 {
		t.Fatal("capability assertion changed network access policy", c)
	}
	for _, value := range []string{"https://user:SECRET@example.test", "https://example.test/SECRET", "https://example.test?SECRET", "ftp://SECRET.example", "http://example.test:SECRET"} {
		result, err := LoadEnv(env(map[string]string{"API_CONDITIONAL_COMPLETE_ENDPOINTS": value}))
		if err == nil || strings.Contains(err.Error(), "SECRET") || !reflect.DeepEqual(result, Config{}) {
			t.Fatal("invalid endpoint assertion accepted/leaked", result, err)
		}
	}
}

func TestConditionalCompleteConfigCloneOwnsAssertionMap(t *testing.T) {
	c := Config{ConditionalCompleteEndpoints: map[string]bool{"https://s3.example": true}}
	copy := c.Clone()
	delete(copy.ConditionalCompleteEndpoints, "https://s3.example")
	copy.ConditionalCompleteEndpoints["http://other:9000"] = true
	if !c.ConditionalCompleteEndpoints["https://s3.example"] || c.ConditionalCompleteEndpoints["http://other:9000"] {
		t.Fatal("configuration capability map escaped its owner", c)
	}
	if (Config{}).Clone().ConditionalCompleteEndpoints != nil {
		t.Fatal("nil security policy changed shape")
	}
}

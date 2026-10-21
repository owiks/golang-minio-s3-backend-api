package config

import (
	"strings"
	"testing"
)

func TestOperationBudgets(t *testing.T) {
	c, err := LoadEnv(func(key string) string {
		switch key {
		case "API_TEMPORARY_BYTES":
			return "33554432"
		case "API_CONDITIONAL_DELETE_ENDPOINTS":
			return "http://minio.example:9000"
		}
		return ""
	})
	if err != nil || c.TemporaryBytes != 33554432 || !c.ConditionalDeleteEndpoints["http://minio.example:9000"] {
		t.Fatal(c.TemporaryBytes, err)
	}
	for _, input := range []struct{ key, value string }{{"API_TEMPORARY_BYTES", "0"}, {"API_TEMPORARY_BYTES", "68719476737"}, {"API_CONDITIONAL_DELETE_ENDPOINTS", "https://user:SECRET@example.test"}} {
		_, err = LoadEnv(func(key string) string {
			if key == input.key {
				return input.value
			}
			return ""
		})
		if err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatal("unsafe configuration accepted", err)
		}
	}
}

package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestDefaultsAndIndependentPolicies(t *testing.T) {
	c, err := LoadEnv(env(nil))
	if err != nil || c.Listen != ":8081" || c.SessionTTL != time.Hour || c.Origin != "" || len(c.Endpoints.Allowed) != 0 || len(c.Endpoints.Overrides) != 0 {
		t.Fatalf("unexpected defaults: %+v, %v", c, err)
	}
	c.Endpoints.Allowed["http://first:9000"] = true
	other := Default()
	if len(other.Endpoints.Allowed) != 0 {
		t.Fatal("configuration maps shared across instances")
	}
}

func TestLoadNormalizesEndpointPolicy(t *testing.T) {
	c, err := LoadEnv(env(map[string]string{
		"API_LISTEN": " 127.0.0.1:9090 ", "APP_ORIGIN": " https://console.example/ ",
		"MINIO_ALLOWED_ENDPOINTS":  " https://S3.EXAMPLE/, ,http://minio:9000 ",
		"MINIO_ENDPOINT_OVERRIDES": `{"https://PUBLIC.example/":"http://MINIO:9000/"}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:9090" || c.Origin != "https://console.example" || !reflect.DeepEqual(c.Endpoints.Allowed, map[string]bool{"https://s3.example": true, "http://minio:9000": true, "https://public.example": true}) || c.Endpoints.Overrides["https://public.example"] != "http://minio:9000" {
		t.Fatalf("incorrect normalization: %+v", c)
	}
}

func TestInvalidConfigFailsWithoutExposingValues(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"listen":              {"API_LISTEN": "secret-value"},
		"origin credentials":  {"APP_ORIGIN": "https://user:secret-value@example.test"},
		"origin path":         {"APP_ORIGIN": "https://example.test/secret-value"},
		"origin query":        {"APP_ORIGIN": "https://example.test?secret-value"},
		"allowed endpoint":    {"MINIO_ALLOWED_ENDPOINTS": "https://valid.test,http://user:secret-value@example.test"},
		"override JSON":       {"MINIO_ENDPOINT_OVERRIDES": "secret-value"},
		"override source":     {"MINIO_ENDPOINT_OVERRIDES": `{"https://example.test/secret-value":"http://minio:9000"}`},
		"override target":     {"MINIO_ENDPOINT_OVERRIDES": `{"https://example.test":"http://user:secret-value@minio:9000"}`},
		"normalized conflict": {"MINIO_ENDPOINT_OVERRIDES": `{"https://EXAMPLE.test":"http://first:9000","https://example.test/":"http://secret-value:9000"}`},
	} {
		t.Run(name, func(t *testing.T) {
			c, err := LoadEnv(env(values))
			if err == nil || strings.Contains(err.Error(), "secret-value") || !reflect.DeepEqual(c, Config{}) {
				t.Fatalf("must fail safely, config=%+v error=%v", c, err)
			}
		})
	}
}

func TestAdmissionConfigValidation(t *testing.T) {
	for key, value := range map[string]string{"API_MAX_TRANSFERS": "0", "API_MAX_TRANSFERS_PER_SESSION": "65", "API_MAX_LOGINS": "invalid-secret-value", "API_LOGIN_BURST": "-1", "API_LOGIN_REFILL": "0s", "API_TRUSTED_PROXIES": "invalid-secret-value"} {
		if _, err := LoadEnv(env(map[string]string{key: value})); err == nil || strings.Contains(err.Error(), "secret-value") {
			t.Fatalf("invalid config accepted or leaked: %s %v", key, err)
		}
	}
	if _, err := LoadEnv(env(map[string]string{"API_MAX_TRANSFERS": "1", "API_MAX_TRANSFERS_PER_SESSION": "2"})); err == nil {
		t.Fatal("session exceeds process")
	}
	c, err := LoadEnv(env(map[string]string{"API_MAX_TRANSFERS": "8", "API_MAX_TRANSFERS_PER_SESSION": "3", "API_MAX_LOGINS": "12", "API_LOGIN_BURST": "20", "API_LOGIN_REFILL": "2s", "API_TRUSTED_PROXIES": "192.0.2.0/24,::1/128"}))
	if err != nil || c.Limits.Transfers != 8 || c.Limits.TransfersPerSession != 3 || c.Limits.Logins != 12 || c.Limits.LoginBurst != 20 || c.Limits.LoginInterval != 2*time.Second || len(c.TrustedProxies) != 2 {
		t.Fatalf("wrong admission config: %+v %v", c, err)
	}
}

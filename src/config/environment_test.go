package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBlankEnvironmentRetainsDefaults(t *testing.T) {
	want, err := LoadEnv(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{
		"API_TOKEN_TTL", "API_LANGUAGE", "API_LISTEN", "APP_ORIGIN",
		"MINIO_ALLOWED_ENDPOINTS", "API_MAX_TRANSFERS", "API_MAX_TRANSFERS_PER_SESSION",
		"API_MAX_LOGINS", "API_LOGIN_BURST", "API_LOGIN_REFILL", "API_TRUSTED_PROXIES",
		"API_TEMPORARY_BYTES", "API_CONDITIONAL_DELETE_ENDPOINTS", "API_CONDITIONAL_COMPLETE_ENDPOINTS",
		"API_DB_DRIVER", "API_DB_HOST", "API_DB_PORT", "API_DB_TLS_MODE", "API_DB_TLS_CA_FILE",
		"API_STATE_KEYRING_FILE", "API_DB_MAX_OPEN", "API_DB_MAX_IDLE", "API_DB_CONN_LIFETIME",
	}
	for _, value := range []string{"", " \t\r\n "} {
		values := map[string]string{}
		for _, key := range keys {
			values[key] = value
		}
		got, err := LoadEnv(env(values))
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("blank settings changed defaults: %+v %v", got, err)
		}
	}
	if _, err := LoadEnv(env(map[string]string{"MINIO_ENDPOINT_OVERRIDES": " \t\n "})); err == nil || err.Error() != "invalid MINIO_ENDPOINT_OVERRIDES" {
		t.Fatal("whitespace JSON must remain invalid", err)
	}
}

func TestTypedEnvironmentPreservesWhitespaceAndDurationContract(t *testing.T) {
	values := map[string]string{
		"API_TOKEN_TTL": " 2s ", "API_MAX_TRANSFERS": " +8 ", "API_MAX_TRANSFERS_PER_SESSION": " 03 ",
		"API_MAX_LOGINS": " 16 ", "API_LOGIN_BURST": " 80 ", "API_LOGIN_REFILL": " 150.5ms ",
		"API_TEMPORARY_BYTES": " +33554432 ", "API_DB_MAX_OPEN": " 12 ", "API_DB_MAX_IDLE": " 4 ",
		"API_DB_CONN_LIFETIME": " 1500ms ",
	}
	got, err := LoadEnv(env(values))
	if err != nil || got.TokenTTL != 2*time.Second || got.Limits.Transfers != 8 || got.Limits.TransfersPerSession != 3 || got.Limits.Logins != 16 || got.Limits.LoginBurst != 80 || got.Limits.LoginInterval != 150500*time.Microsecond || got.TemporaryBytes != 33554432 || got.State.MaxOpen != 12 || got.State.MaxIdle != 4 || got.State.Lifetime != 1500*time.Millisecond {
		t.Fatalf("typed settings changed: %+v %v", got, err)
	}
	if _, err := LoadEnv(env(map[string]string{"API_TOKEN_TTL": "1500ms"})); err == nil || err.Error() != "invalid API_TOKEN_TTL" {
		t.Fatal("token lifetime must remain whole seconds", err)
	}
}

func TestEnvironmentParseErrorsRemainSafeAndOrdered(t *testing.T) {
	for _, key := range []string{
		"API_TOKEN_TTL", "API_MAX_TRANSFERS", "API_MAX_TRANSFERS_PER_SESSION", "API_MAX_LOGINS",
		"API_LOGIN_BURST", "API_LOGIN_REFILL", "API_TEMPORARY_BYTES", "API_DB_PORT",
		"API_DB_MAX_OPEN", "API_DB_MAX_IDLE", "API_DB_CONN_LIFETIME",
	} {
		for _, value := range []string{"private-secret-value", "9223372036854775808"} {
			got, err := LoadEnv(env(map[string]string{key: value}))
			if err == nil || err.Error() != "invalid "+key || !reflect.DeepEqual(got, Config{}) {
				t.Fatalf("unsafe parse failure for %s: %+v %v", key, got, err)
			}
		}
	}
	for _, tc := range []struct {
		values map[string]string
		want   string
	}{
		{map[string]string{"API_TOKEN_TTL": "1500ms", "API_LOGIN_REFILL": "secret"}, "invalid API_TOKEN_TTL"},
		{map[string]string{"API_LANGUAGE": "de", "API_MAX_TRANSFERS": "secret"}, "invalid API_LANGUAGE"},
		{map[string]string{"APP_ORIGIN": "https://user:secret@example.test", "API_TEMPORARY_BYTES": "secret"}, "invalid APP_ORIGIN"},
		{map[string]string{"API_MAX_TRANSFERS": "1", "API_MAX_LOGINS": "secret"}, "invalid API_MAX_LOGINS"},
		{map[string]string{"API_MAX_TRANSFERS": "1", "API_LOGIN_REFILL": "secret"}, "API_MAX_TRANSFERS_PER_SESSION exceeds API_MAX_TRANSFERS"},
		{map[string]string{"API_DB_MAX_OPEN": "1", "API_DB_CONN_LIFETIME": "secret"}, "API_DB_MAX_IDLE exceeds API_DB_MAX_OPEN"},
		{map[string]string{"API_DB_DRIVER": "sqlite", "API_DB_SQLITE_PATH": "/tmp/state.db", "API_DB_MAX_OPEN": "secret"}, "persistent API_DB_DRIVER requires API_STATE_KEYRING_FILE"},
	} {
		_, err := LoadEnv(env(tc.values))
		if err == nil || err.Error() != tc.want {
			t.Fatalf("first validation error changed: want=%q got=%v", tc.want, err)
		}
	}
}

func TestDatabaseEnvironmentKeepsLiteralFieldsAndCustomPrefix(t *testing.T) {
	for _, prefix := range []string{"API_DB_", "API_TARGET_DB_"} {
		values := map[string]string{
			prefix + "DRIVER": " postgres ", prefix + "HOST": " ::1 ", prefix + "PORT": " +5433 ",
			prefix + "NAME": "  database ${NAME}  ", prefix + "USER": "  user:${USER}  ",
			prefix + "PASSWORD": "  ${PASSWORD}:/@?#%\t\n  ", prefix + "TLS_MODE": " verify-full ",
			prefix + "TLS_CA_FILE": " /tmp/database-ca.pem ",
			"API_LANGUAGE":         "invalid-secret", "API_DB_MAX_OPEN": "invalid-secret",
		}
		got, err := LoadDatabaseEnv(env(values), prefix)
		if err != nil || got.Driver != "postgres" || got.Host != "::1" || got.Port != 5433 || got.TLSMode != "verify-full" || got.TLSCAFile != "/tmp/database-ca.pem" || got.Name != values[prefix+"NAME"] || got.User != values[prefix+"USER"] || got.Password != values[prefix+"PASSWORD"] {
			t.Fatalf("database fields changed for %s: %v", prefix, err)
		}
		for _, value := range []string{"0", "65536", "private-secret"} {
			values[prefix+"PORT"] = value
			if _, err := LoadDatabaseEnv(env(values), prefix); err == nil || err.Error() != "invalid "+prefix+"PORT" || strings.Contains(err.Error(), "private-secret") {
				t.Fatal("database error changed or leaked a value", err)
			}
		}
		values = map[string]string{prefix + "DRIVER": "sqlite", prefix + "SQLITE_PATH": " /tmp/${STATE} database "}
		got, err = LoadDatabaseEnv(env(values), prefix)
		if err != nil || got.SQLitePath != values[prefix+"SQLITE_PATH"] {
			t.Fatal("SQLite path was normalized or expanded", err)
		}
	}
}

func TestEnvironmentNeverFallsBackToProcessValues(t *testing.T) {
	t.Setenv("API_TOKEN_TTL", "private-secret")
	t.Setenv("API_DB_DRIVER", "postgres")
	t.Setenv("API_DB_PASSWORD", "private-secret")
	got, err := LoadEnv(env(nil))
	if err != nil || got.TokenTTL != time.Hour || got.State.Driver != "memory" || got.State.Password != "" {
		t.Fatal("configuration read unsupplied process variables", err)
	}
}

package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStateConfiguration(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres", "mysql"} {
		values := databaseEnvironment(driver)
		cfg, err := LoadEnv(func(k string) string { return values[k] })
		if err != nil || cfg.State.Driver != driver {
			t.Fatal(driver, err)
		}
		if cfg.State.Password != values["API_DB_PASSWORD"] {
			t.Fatal("password was modified")
		}
		if driver == "postgres" && cfg.State.Port != 5432 || driver == "mysql" && cfg.State.Port != 3306 {
			t.Fatal("incorrect default port")
		}
		raw, err := json.Marshal(cfg)
		if err != nil || strings.Contains(string(raw), "private-password") {
			t.Fatal("configuration exposed password", err)
		}
		delete(values, "API_STATE_KEYRING_FILE")
		if _, err = LoadEnv(func(k string) string { return values[k] }); err == nil {
			t.Fatal("persistent state without key accepted")
		}
	}
}

func TestInvalidDatabaseConfiguration(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"API_DB_DRIVER", "unknown"}, {"API_DB_HOST", "db.example:5432"},
		{"API_DB_HOST", "https://db.example"}, {"API_DB_PORT", "0"},
		{"API_DB_PORT", "65536"}, {"API_DB_PORT", "invalid"},
		{"API_DB_NAME", ""}, {"API_DB_USER", ""}, {"API_DB_PASSWORD", ""},
		{"API_DB_TLS_MODE", "insecure"}, {"API_DB_DSN_FILE", "private-password"},
		{"API_STATE_DRIVER", "postgres"}, {"API_STATE_DSN", "private-password"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			values := databaseEnvironment("postgres")
			values[tc.key] = tc.value
			_, err := LoadEnv(func(k string) string { return values[k] })
			if err == nil || strings.Contains(err.Error(), "private-password") {
				t.Fatal("invalid configuration accepted or secret leaked", err)
			}
		})
	}
	for _, path := range []string{"", ":memory:"} {
		values := databaseEnvironment("sqlite")
		values["API_DB_SQLITE_PATH"] = path
		if _, err := LoadEnv(func(k string) string { return values[k] }); err == nil {
			t.Fatal("invalid persistent SQLite path accepted")
		}
	}
}

func TestTargetDatabaseEnvironmentIsIndependent(t *testing.T) {
	values := map[string]string{
		"API_DB_DRIVER": "sqlite", "API_DB_SQLITE_PATH": "/tmp/source.db",
		"API_TARGET_DB_DRIVER": "mysql", "API_TARGET_DB_HOST": "::1", "API_TARGET_DB_PORT": "13306",
		"API_TARGET_DB_USER": "target-user", "API_TARGET_DB_PASSWORD": "  private-password:/@?#%  ", "API_TARGET_DB_NAME": "target",
	}
	d, err := LoadDatabaseEnv(func(k string) string { return values[k] }, "API_TARGET_DB_")
	if err != nil || d.Driver != "mysql" || d.Host != "::1" || d.Port != 13306 || d.Password != values["API_TARGET_DB_PASSWORD"] || d.TLSMode != "verify-full" {
		t.Fatal("target database fields/defaults invalid", err)
	}
}

func databaseEnvironment(driver string) map[string]string {
	return map[string]string{
		"API_DB_DRIVER": driver, "API_DB_HOST": "db.example", "API_DB_NAME": "minio_api",
		"API_DB_USER": "api", "API_DB_PASSWORD": "  private-password:/@?#%  ",
		"API_DB_SQLITE_PATH": "/tmp/state.db", "API_STATE_KEYRING_FILE": "/run/secrets/keyring",
	}
}

package config

import (
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

func loadState(c *Config, env func(string) string) error {
	s := &c.State
	for _, legacy := range []string{"API_STATE_DRIVER", "API_STATE_DSN", "API_STATE_DSN_FILE", "API_DB_DSN", "API_DB_DSN_FILE"} {
		if env(legacy) != "" {
			return errors.New(legacy + " is no longer supported; configure API_DB_DRIVER, HOST, PORT, NAME, USER and PASSWORD")
		}
	}
	var err error
	s.Database, err = LoadDatabaseEnv(env, "API_DB_")
	if err != nil {
		return err
	}
	s.KeyringFile = strings.TrimSpace(env("API_STATE_KEYRING_FILE"))
	if s.Driver != "memory" && s.KeyringFile == "" {
		return errors.New("persistent API_DB_DRIVER requires API_STATE_KEYRING_FILE")
	}
	for _, o := range []struct {
		name   string
		target *int
	}{{"API_DB_MAX_OPEN", &s.MaxOpen}, {"API_DB_MAX_IDLE", &s.MaxIdle}} {
		if v := strings.TrimSpace(env(o.name)); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 100 {
				return errors.New("invalid " + o.name)
			}
			*o.target = n
		}
	}
	if s.MaxIdle > s.MaxOpen {
		return errors.New("API_DB_MAX_IDLE exceeds API_DB_MAX_OPEN")
	}
	if v := strings.TrimSpace(env("API_DB_CONN_LIFETIME")); v != "" {
		d, e := time.ParseDuration(v)
		if e != nil || d < time.Second || d > 24*time.Hour {
			return errors.New("invalid API_DB_CONN_LIFETIME")
		}
		s.Lifetime = d
	}
	return nil
}

// LoadDatabaseEnv uses the same field contract for the API and offline transfer.
// Passwords are kept verbatim; validation errors contain variable names only.
func LoadDatabaseEnv(env func(string) string, prefix string) (Database, error) {
	d := Database{
		Driver: strings.TrimSpace(env(prefix + "DRIVER")),
		Host:   strings.TrimSpace(env(prefix + "HOST")),
		Name:   env(prefix + "NAME"), User: env(prefix + "USER"), Password: env(prefix + "PASSWORD"),
		SQLitePath: env(prefix + "SQLITE_PATH"),
		TLSMode:    strings.TrimSpace(env(prefix + "TLS_MODE")),
		TLSCAFile:  strings.TrimSpace(env(prefix + "TLS_CA_FILE")),
	}
	if port := strings.TrimSpace(env(prefix + "PORT")); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return Database{}, errors.New("invalid " + prefix + "PORT")
		}
		d.Port = n
	}
	return d.validated(prefix)
}

func LoadTransferDatabase() (Database, error) {
	return LoadDatabaseEnv(os.Getenv, "API_TARGET_DB_")
}

// Migration commands only need database access, not the encryption keyring or HTTP settings.
func LoadMigrationDatabase(target bool) (Database, error) {
	if target {
		return LoadTransferDatabase()
	}
	return LoadDatabaseEnv(os.Getenv, "API_DB_")
}

// Validated applies the same defaults and validation to embedded callers.
func (d Database) Validated() (Database, error) { return d.validated("API_DB_") }

func (d Database) validated(prefix string) (Database, error) {
	if d.Driver == "" {
		d.Driver = "memory"
	}
	if d.TLSMode == "" {
		d.TLSMode = "verify-full"
	}
	if d.TLSMode != "disable" && d.TLSMode != "verify-full" {
		return Database{}, errors.New("invalid " + prefix + "TLS_MODE")
	}
	if d.Driver == "memory" {
		return d, nil
	}
	if d.Driver == "sqlite" {
		if d.SQLitePath == "" || d.SQLitePath == ":memory:" || len(d.SQLitePath) > 4096 || strings.ContainsRune(d.SQLitePath, 0) {
			return Database{}, errors.New("invalid " + prefix + "SQLITE_PATH")
		}
		return d, nil
	}
	switch d.Driver {
	case "postgres":
		if d.Port == 0 {
			d.Port = 5432
		}
	case "mysql":
		if d.Port == 0 {
			d.Port = 3306
		}
	default:
		return Database{}, errors.New("invalid " + prefix + "DRIVER")
	}
	if d.Host == "" {
		d.Host = "127.0.0.1"
	}
	if strings.ContainsAny(d.Host, " \t\r\n/@?#[]\\\x00") || strings.Contains(d.Host, ":") && net.ParseIP(d.Host) == nil {
		return Database{}, errors.New("invalid " + prefix + "HOST")
	}
	if d.Port < 1 || d.Port > 65535 {
		return Database{}, errors.New("invalid " + prefix + "PORT")
	}
	for _, field := range []struct{ name, value string }{{"NAME", d.Name}, {"USER", d.User}, {"PASSWORD", d.Password}} {
		if field.value == "" || len(field.value) > 65536 || strings.ContainsRune(field.value, 0) {
			return Database{}, errors.New("invalid " + prefix + field.name)
		}
	}
	if d.TLSMode == "disable" && d.TLSCAFile != "" {
		return Database{}, errors.New(prefix + "TLS_CA_FILE requires verified TLS")
	}
	return d, nil
}

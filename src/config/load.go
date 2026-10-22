// Package config loads process configuration once, before accepting requests.
package config

import (
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/endpoint"
)

func Load() (Config, error) { return LoadEnv(os.Getenv) }

// LoadEnv permits isolated configuration tests without changing process state.
// Errors contain variable names only; environment values can include secrets.
func LoadEnv(getenv func(string) string) (Config, error) {
	c := Default()
	if err := loadState(&c, getenv); err != nil {
		return Config{}, err
	}
	invalid, err := parseEnvironment(&c, getenv, "")
	if err != nil {
		return Config{}, err
	}
	for _, option := range []struct{ field, name string }{
		{"ReadOnly", "API_READ_ONLY"},
		{"DisableUploads", "API_DISABLE_UPLOADS"},
		{"DisableDeletes", "API_DISABLE_DELETES"},
		{"DisableAdminChanges", "API_DISABLE_ADMIN_CHANGES"},
	} {
		if invalid[option.field] {
			return Config{}, errors.New("invalid " + option.name)
		}
	}
	if invalid["TokenTTL"] || c.TokenTTL < time.Second || c.TokenTTL > 24*time.Hour || c.TokenTTL%time.Second != 0 {
		return Config{}, errors.New("invalid API_TOKEN_TTL")
	}
	if c.Language != "auto" && c.Language != "en" && c.Language != "ru" {
		return Config{}, errors.New("invalid API_LANGUAGE")
	}
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return Config{}, errors.New("invalid API_LISTEN")
	}
	c.Origin = strings.TrimRight(c.Origin, "/")
	if c.Origin != "" {
		u, err := url.Parse(c.Origin)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
			return Config{}, errors.New("invalid APP_ORIGIN")
		}
	}
	for _, value := range strings.Split(getenv("MINIO_ALLOWED_ENDPOINTS"), ",") {
		if strings.TrimSpace(value) == "" {
			continue
		}
		u, err := endpoint.Parse(value)
		if err != nil {
			return Config{}, errors.New("invalid MINIO_ALLOWED_ENDPOINTS")
		}
		c.Endpoints.Allowed[u.String()] = true
	}
	if raw := getenv("MINIO_ENDPOINT_OVERRIDES"); raw != "" {
		var overrides map[string]string
		if json.Unmarshal([]byte(raw), &overrides) != nil {
			return Config{}, errors.New("invalid MINIO_ENDPOINT_OVERRIDES")
		}
		for from, to := range overrides {
			a, err := endpoint.Parse(from)
			if err != nil {
				return Config{}, errors.New("invalid endpoint override source")
			}
			b, err := endpoint.Parse(to)
			if err != nil {
				return Config{}, errors.New("invalid endpoint override target")
			}
			if previous, exists := c.Endpoints.Overrides[a.String()]; exists && previous != b.String() {
				return Config{}, errors.New("conflicting endpoint overrides")
			}
			c.Endpoints.Overrides[a.String()] = b.String()
			c.Endpoints.Allowed[a.String()] = true
		}
	}

	for _, option := range []struct {
		name    string
		field   string
		value   int
		maximum int
	}{
		{"API_MAX_TRANSFERS", "Transfers", c.Limits.Transfers, 64},
		{"API_MAX_TRANSFERS_PER_SESSION", "TransfersPerSession", c.Limits.TransfersPerSession, 32},
		{"API_MAX_LOGINS", "Logins", c.Limits.Logins, 64},
		{"API_LOGIN_BURST", "LoginBurst", c.Limits.LoginBurst, 1000},
	} {
		if invalid[option.field] || option.value < 1 || option.value > option.maximum {
			return Config{}, errors.New("invalid " + option.name)
		}
	}
	if c.Limits.TransfersPerSession > c.Limits.Transfers {
		return Config{}, errors.New("API_MAX_TRANSFERS_PER_SESSION exceeds API_MAX_TRANSFERS")
	}
	if invalid["LoginInterval"] || c.Limits.LoginInterval < time.Millisecond*100 || c.Limits.LoginInterval > time.Minute {
		return Config{}, errors.New("invalid API_LOGIN_REFILL")
	}
	for _, raw := range strings.Split(getenv("API_TRUSTED_PROXIES"), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return Config{}, errors.New("invalid API_TRUSTED_PROXIES")
		}
		c.TrustedProxies = append(c.TrustedProxies, prefix.Masked())
	}
	if invalid["TemporaryBytes"] || c.TemporaryBytes < 16*1024*1024 || c.TemporaryBytes > 64*1024*1024*1024 {
		return Config{}, errors.New("invalid API_TEMPORARY_BYTES")
	}

	c.ConditionalDeleteEndpoints = map[string]bool{}
	for _, raw := range strings.Split(getenv("API_CONDITIONAL_DELETE_ENDPOINTS"), ",") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		parsed, err := endpoint.Parse(raw)
		if err != nil {
			return Config{}, errors.New("invalid API_CONDITIONAL_DELETE_ENDPOINTS")
		}
		c.ConditionalDeleteEndpoints[parsed.String()] = true
	}
	c.ConditionalCompleteEndpoints = map[string]bool{}
	for _, raw := range strings.Split(getenv("API_CONDITIONAL_COMPLETE_ENDPOINTS"), ",") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		parsed, err := endpoint.Parse(raw)
		if err != nil {
			return Config{}, errors.New("invalid API_CONDITIONAL_COMPLETE_ENDPOINTS")
		}
		c.ConditionalCompleteEndpoints[parsed.String()] = true
	}
	return c, nil
}

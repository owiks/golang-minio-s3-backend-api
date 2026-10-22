// Package config loads process configuration once, before accepting requests.
package config

import (
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/endpoint"
)

func Load() (Config, error) { return LoadEnv(os.Getenv) }

// LoadEnv permits isolated configuration tests without changing process state.
// Errors contain variable names only; environment values can include secrets.
func LoadEnv(getenv func(string) string) (Config, error) {
	c := Default()
	if raw := strings.TrimSpace(getenv("API_TOKEN_TTL")); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value < time.Second || value > 24*time.Hour || value%time.Second != 0 {
			return Config{}, errors.New("invalid API_TOKEN_TTL")
		}
		c.TokenTTL = value
	}
	if raw := strings.TrimSpace(getenv("API_LANGUAGE")); raw != "" {
		if raw != "auto" && raw != "en" && raw != "ru" {
			return Config{}, errors.New("invalid API_LANGUAGE")
		}
		c.Language = raw
	}
	if raw := strings.TrimSpace(getenv("API_LISTEN")); raw != "" {
		if _, _, err := net.SplitHostPort(raw); err != nil {
			return Config{}, errors.New("invalid API_LISTEN")
		}
		c.Listen = raw
	}
	c.Origin = strings.TrimRight(strings.TrimSpace(getenv("APP_ORIGIN")), "/")
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
		target  *int
		maximum int
	}{
		{"API_MAX_TRANSFERS", &c.Limits.Transfers, 64},
		{"API_MAX_TRANSFERS_PER_SESSION", &c.Limits.TransfersPerSession, 32},
		{"API_MAX_LOGINS", &c.Limits.Logins, 64},
		{"API_LOGIN_BURST", &c.Limits.LoginBurst, 1000},
	} {
		if raw := strings.TrimSpace(getenv(option.name)); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 || value > option.maximum {
				return Config{}, errors.New("invalid " + option.name)
			}
			*option.target = value
		}
	}
	if c.Limits.TransfersPerSession > c.Limits.Transfers {
		return Config{}, errors.New("API_MAX_TRANSFERS_PER_SESSION exceeds API_MAX_TRANSFERS")
	}
	if raw := strings.TrimSpace(getenv("API_LOGIN_REFILL")); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value < time.Millisecond*100 || value > time.Minute {
			return Config{}, errors.New("invalid API_LOGIN_REFILL")
		}
		c.Limits.LoginInterval = value
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
	if raw := strings.TrimSpace(getenv("API_TEMPORARY_BYTES")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 16*1024*1024 || value > 64*1024*1024*1024 {
			return Config{}, errors.New("invalid API_TEMPORARY_BYTES")
		}
		c.TemporaryBytes = value
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

package config

import (
	"strings"
	"testing"
	"time"
)

func TestTokenTTLConfiguration(t *testing.T) {
	for _, value := range []string{"", "1s", "15m", "24h"} {
		cfg, err := LoadEnv(env(map[string]string{"API_TOKEN_TTL": value}))
		want := time.Hour
		if value != "" {
			want, _ = time.ParseDuration(value)
		}
		if err != nil || cfg.TokenTTL != want || cfg.SessionTTL != time.Hour {
			t.Fatal("token lifetime changed cookie defaults", value, err)
		}
	}
	for _, value := range []string{"secret-invalid", "0", "-1s", "500ms", "1.5s", "24h1s"} {
		if _, err := LoadEnv(env(map[string]string{"API_TOKEN_TTL": value})); err == nil || strings.Contains(err.Error(), value) {
			t.Fatal("invalid lifetime accepted or exposed", err)
		}
	}
}

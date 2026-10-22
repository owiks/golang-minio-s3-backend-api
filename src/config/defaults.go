package config

import (
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/endpoint"
)

func Default() Config {
	return Config{
		Language:       "auto",
		Listen:         ":8081",
		Endpoints:      endpoint.Policy{Allowed: map[string]bool{}, Overrides: map[string]string{}},
		SessionTTL:     time.Hour,
		TokenTTL:       time.Hour,
		TemporaryBytes: 2 * 1024 * 1024 * 1024,
		Limits:         Limits{Transfers: 4, TransfersPerSession: 2, Logins: 8, LoginBurst: 60, LoginInterval: time.Second},
	}
}

// WithDefaults supports embedded callers constructing a partial Config.
func (c Config) WithDefaults() Config {
	d := Default()
	if c.Language == "" {
		c.Language = d.Language
	}
	if c.TemporaryBytes <= 0 {
		c.TemporaryBytes = d.TemporaryBytes
	}
	if c.SessionTTL <= 0 {
		c.SessionTTL = d.SessionTTL
	}
	if c.TokenTTL <= 0 {
		c.TokenTTL = d.TokenTTL
	}
	if c.Limits.Transfers <= 0 {
		c.Limits.Transfers = d.Limits.Transfers
	}
	if c.Limits.TransfersPerSession <= 0 {
		c.Limits.TransfersPerSession = d.Limits.TransfersPerSession
	}
	if c.Limits.Logins <= 0 {
		c.Limits.Logins = d.Limits.Logins
	}
	if c.Limits.LoginBurst <= 0 {
		c.Limits.LoginBurst = d.Limits.LoginBurst
	}
	if c.Limits.LoginInterval <= 0 {
		c.Limits.LoginInterval = d.Limits.LoginInterval
	}
	return c
}

package config

import (
	"maps"
	"net/netip"
	"slices"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/endpoint"
)

type Config struct {
	Language                     string
	Listen                       string
	Origin                       string
	Endpoints                    endpoint.Policy
	SessionTTL                   time.Duration
	TemporaryBytes               int64
	Limits                       Limits
	TrustedProxies               []netip.Prefix
	ConditionalDeleteEndpoints   map[string]bool
	ConditionalCompleteEndpoints map[string]bool
}

// Clone detaches mutable security settings. Callers own the returned snapshot.
func (c Config) Clone() Config {
	c.Endpoints.Allowed = maps.Clone(c.Endpoints.Allowed)
	c.Endpoints.Overrides = maps.Clone(c.Endpoints.Overrides)
	c.TrustedProxies = slices.Clone(c.TrustedProxies)
	c.ConditionalDeleteEndpoints = maps.Clone(c.ConditionalDeleteEndpoints)
	c.ConditionalCompleteEndpoints = maps.Clone(c.ConditionalCompleteEndpoints)
	return c
}

// Limits are process-local budgets; sessions cannot consume all transfer slots.
type Limits struct {
	Transfers           int
	TransfersPerSession int
	Logins              int
	LoginBurst          int
	LoginInterval       time.Duration
}

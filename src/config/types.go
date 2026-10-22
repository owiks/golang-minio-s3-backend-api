package config

import (
	"maps"
	"net/netip"
	"slices"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/endpoint"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operationpolicy"
)

type Config struct {
	State State `env:"-"`

	Language                     string          `env:"API_LANGUAGE"`
	Listen                       string          `env:"API_LISTEN"`
	Origin                       string          `env:"APP_ORIGIN"`
	Endpoints                    endpoint.Policy `env:"-"`
	SessionTTL                   time.Duration   `env:"-"`
	TokenTTL                     time.Duration   `env:"API_TOKEN_TTL"`
	OperatorToken                string          `env:"API_OPERATOR_TOKEN" json:"-"`
	TemporaryBytes               int64           `env:"API_TEMPORARY_BYTES"`
	Operations                   operationpolicy.Policy
	Limits                       Limits
	TrustedProxies               []netip.Prefix  `env:"-"`
	ConditionalDeleteEndpoints   map[string]bool `env:"-"`
	ConditionalCompleteEndpoints map[string]bool `env:"-"`
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
	Transfers           int           `env:"API_MAX_TRANSFERS"`
	TransfersPerSession int           `env:"API_MAX_TRANSFERS_PER_SESSION"`
	Logins              int           `env:"API_MAX_LOGINS"`
	LoginBurst          int           `env:"API_LOGIN_BURST"`
	LoginInterval       time.Duration `env:"API_LOGIN_REFILL"`
}

// Database describes connection fields without exposing a driver-specific DSN.
type Database struct {
	Driver     string `env:"DRIVER"`
	Host       string `env:"HOST"`
	Port       int    `env:"PORT"`
	Name       string `env:"NAME"`
	User       string `env:"USER"`
	Password   string `env:"PASSWORD" json:"-"`
	SQLitePath string `env:"SQLITE_PATH"`
	TLSMode    string `env:"TLS_MODE"`
	TLSCAFile  string `env:"TLS_CA_FILE"`
}

// State selects the persistence adapter and encryption settings.
type State struct {
	Database    `env:"-"`
	KeyringFile string        `env:"API_STATE_KEYRING_FILE"`
	MaxOpen     int           `env:"API_DB_MAX_OPEN"`
	MaxIdle     int           `env:"API_DB_MAX_IDLE"`
	Lifetime    time.Duration `env:"API_DB_CONN_LIFETIME"`
}

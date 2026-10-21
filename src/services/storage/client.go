// Package storage constructs SDK clients with consistent signing and retry options.
// It accepts checked endpoints and transports; it never reads environment variables.
package storage

import (
	"context"
	"net/http"
	"net/url"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
)

type Config struct {
	Endpoint    *url.URL
	Credentials *credentials.Credentials
	Region      string
}

func NewS3(c Config, transport http.RoundTripper) (*minio.Client, error) {
	if transport == nil {
		var err error
		transport, err = minio.DefaultTransport(c.Endpoint.Scheme == "https")
		if err != nil {
			return nil, err
		}
	}
	transport = s3ResponseTransport{base: observeTransport(transport)}
	return minio.New(c.Endpoint.Host, &minio.Options{
		Creds: c.Credentials, Secure: c.Endpoint.Scheme == "https", Region: c.Region,
		BucketLookup: minio.BucketLookupPath, MaxRetries: 1, Transport: transport,
	})
}

func NewAdmin(c Config, transport http.RoundTripper) (*AdminClient, error) {
	if transport == nil {
		transport = madmin.DefaultTransport(c.Endpoint.Scheme == "https")
	}
	transport = adminTransport{base: adminResponseTransport{base: observeTransport(transport)}}
	client, err := madmin.NewWithOptions(c.Endpoint.Host, &madmin.Options{
		Creds: c.Credentials, Secure: c.Endpoint.Scheme == "https", Transport: transport,
	})
	if err != nil {
		return nil, err
	}
	return &AdminClient{AdminClient: client}, nil
}

// WithContext also bounds SDK Core operations that do not accept a context.
// It shares the session connection pool rather than allocating another one.
func WithContext(ctx context.Context, base http.RoundTripper) http.RoundTripper {
	return requestTransport{ctx: ctx, base: base}
}

type requestTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

// Wrappers that consume response bytes must observe their base, not a replay of
// already buffered bytes. Context binding likewise precedes observation.
func observeTransport(base http.RoundTripper) http.RoundTripper {
	if wrapper, ok := base.(interface{ withTelemetry() http.RoundTripper }); ok {
		return wrapper.withTelemetry()
	}
	if _, ok := base.(telemetry.Transport); ok {
		return base
	}
	return telemetry.Transport{Base: base}
}

func (t requestTransport) withTelemetry() http.RoundTripper {
	t.base = observeTransport(t.base)
	return t
}

func (t requestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(r.Clone(t.ctx))
}

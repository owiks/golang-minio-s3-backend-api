// Package testsession constructs ordinary sessions for isolated tests without
// granting test code setters or access to live runtime state.
package testsession

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"minio-web/api/src/config"
	"minio-web/api/src/services/runtime"
)

func New(t testing.TB, endpoint string) *runtime.Session {
	t.Helper()
	return With(t, runtime.Connection{Endpoint: endpoint}, config.Config{}, nil)
}

func Native(t testing.TB) *runtime.Session {
	t.Helper()
	return With(t, runtime.Connection{Endpoint: "http://127.0.0.1:19000", AccessKey: "minio-web-test", SecretKey: "minio-web-test-secret"}, config.Config{}, nil)
}

func With(t testing.TB, c runtime.Connection, cfg config.Config, transport *http.Transport) *runtime.Session {
	t.Helper()
	if c.Endpoint == "" {
		c.Endpoint = "http://fixture.example"
	}
	if c.AccessKey == "" {
		c.AccessKey = "fixture"
	}
	if c.SecretKey == "" {
		c.SecretKey = "fixture-secret"
	}
	if c.Region == "" {
		c.Region = "us-east-1"
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = time.Hour
	}
	endpoint, err := url.Parse(c.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if transport == nil {
		transport = http.DefaultTransport.(*http.Transport).Clone()
	}
	t.Cleanup(transport.CloseIdleConnections)
	session, err := runtime.NewConnectedSession(c, cfg, endpoint, endpoint, transport)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

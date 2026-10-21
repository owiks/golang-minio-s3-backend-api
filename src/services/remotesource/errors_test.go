package remotesource

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"

	"minio-web/api/src/services/apperror"
	"minio-web/api/tests/testmessage"
)

type errorTransport struct{ cause error }

func (tr errorTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, tr.cause }
func TestOpenPreservesSafeCauseAndCancellation(t *testing.T) {
	dns := &net.DNSError{Name: "PRIVATE-HOST", Err: "PRIVATE-DETAIL"}
	c := &Client{http: &http.Client{Transport: errorTransport{dns}}}
	_, err := c.Open(context.Background(), "https://example.com/file?token=PRIVATE-TOKEN")
	if apperror.Public(err).DiagnosticCause().Class != "dns" {
		t.Fatal("DNS cause lost", err)
	}
	safe := apperror.Public(err)
	if strings.Contains(testmessage.Russian(safe.Message), "PRIVATE") || strings.Contains(testmessage.Russian(safe.Message), "token=") {
		t.Fatal("private error leaked", safe)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Open(ctx, "https://example.com/file")
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
	_, err = dialPublic(context.Background(), "tcp", "example.com:443", func(context.Context, string) ([]net.IPAddr, error) { return nil, dns }, nil)
	if apperror.Public(err).DiagnosticCause().Class != "dns" {
		t.Fatal("resolver cause lost", err)
	}
	_, err = dialPublic(ctx, "tcp", "example.com:443", func(context.Context, string) ([]net.IPAddr, error) { return nil, ctx.Err() }, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("resolver cancellation lost", err)
	}
}

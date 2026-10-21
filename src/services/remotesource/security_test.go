package remotesource

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"minio-web/api/src/services/apperror"
	"minio-web/api/tests/testmessage"
)

type countedBody struct {
	io.Reader
	reads, closes int
}

func (b *countedBody) Read(p []byte) (int, error) { b.reads++; return b.Reader.Read(p) }
func (b *countedBody) Close() error               { b.closes++; return nil }

func TestOpenMapsHTTPAndTransfersBodyOwnership(t *testing.T) {
	for _, status := range []int{200, 206, 301, 403, 404, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			body := &countedBody{Reader: strings.NewReader("body")}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			client := &Client{http: &http.Client{Transport: remoteTestTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Context() != ctx || r.Method != "GET" || r.URL.String() != "https://example.org/a%20b?token=signed" || r.Header.Get("Accept-Encoding") != "identity" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "" {
					t.Error("source scope, context or credential isolation changed")
				}
				return &http.Response{StatusCode: status, ContentLength: 4, Body: body, Header: http.Header{"Content-Type": {"application/json; charset=utf-8"}}}, nil
			})}}
			file, err := client.Open(ctx, "https://example.org/a%20b?token=signed")
			if calls != 1 || body.reads != 0 {
				t.Fatal("adapter read body or repeated request")
			}
			if status != 200 {
				if err == nil || apperror.Public(err).Code != "SourceResponse" || file.Body != nil || body.closes != 1 {
					t.Fatal("bad response retained body", err, body.closes)
				}
				return
			}
			if err != nil || file.Body != body || file.Size != 4 || file.ContentType != "application/json; charset=utf-8" || body.closes != 0 {
				t.Fatal("body ownership or metadata changed", err)
			}
			if err := file.Body.Close(); err != nil || body.closes != 1 {
				t.Fatal("caller cannot close source")
			}
		})
	}
}

func TestOpenRejectsInvalidInputAndHidesTransportErrors(t *testing.T) {
	calls := 0
	client := &Client{http: &http.Client{Transport: remoteTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("SECRET-UPSTREAM-TOKEN https://private.example")
	})}}
	for _, raw := range []string{"http://example.org/a", "https://user:secret@example.org/a", "https://example.org:444/a", "https://example.org/a#token", "file:///private", "https://example.org/" + strings.Repeat("x", 8192)} {
		_, err := client.Open(context.Background(), raw)
		if err == nil || apperror.Public(err).Code != "InvalidSourceURL" || calls != 0 {
			t.Fatal("invalid URL reached HTTP", err, calls)
		}
	}
	_, err := client.Open(context.Background(), "https://example.org/a?token=sensitive")
	if err == nil || apperror.Public(err).Code != "SourceUnavailable" || strings.Contains(testmessage.Russian(apperror.Public(err).Message), "SECRET") || strings.Contains(testmessage.Russian(apperror.Public(err).Message), "sensitive") || calls != 1 {
		t.Fatal("transport failure leaked or retried")
	}
}

func TestPublicSourceTransportAndRedirectPolicy(t *testing.T) {
	client := New()
	defer client.Close()
	tr := client.http.Transport.(*http.Transport)
	if tr.Proxy != nil || !tr.DisableCompression || tr.TLSClientConfig.MinVersion != tls.VersionTLS12 || tr.TLSHandshakeTimeout != 10*time.Second || tr.ResponseHeaderTimeout != 20*time.Second || tr.IdleConnTimeout != time.Second || client.http.Timeout != 15*time.Minute || client.http.Jar != nil {
		t.Fatal("source transport safety options changed")
	}
	for _, tc := range []struct {
		raw       string
		redirects int
		code      string
	}{
		{"https://example.org/next", 4, ""},
		{"https://example.org/next", 5, "TooManyRedirects"},
		{"http://example.org/next", 1, "InvalidSourceURL"},
		{"https://example.org:8443/next", 1, "InvalidSourceURL"},
		{"https://user:password@example.org/next", 1, "InvalidSourceURL"},
		{"https://example.org/next#secret", 1, "InvalidSourceURL"},
	} {
		u, _ := url.Parse(tc.raw)
		r := &http.Request{URL: u, Header: http.Header{"Authorization": {"SECRET"}, "Cookie": {"SECRET"}, "Referer": {"SECRET"}}}
		err := client.http.CheckRedirect(r, make([]*http.Request, tc.redirects))
		if tc.code == "" && err != nil || tc.code != "" && (err == nil || apperror.Public(err).Code != tc.code) {
			t.Fatal("redirect rule changed", err)
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "" {
			t.Fatal("redirect retained sensitive headers")
		}
	}
}

func TestOpenEnforcesRedirectLimitBeforeAnotherRequest(t *testing.T) {
	client := New()
	defer client.Close()
	requests := 0
	client.http.Transport = remoteTestTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Header.Get("Referer") != "" || r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("redirect headers changed")
		}
		return &http.Response{StatusCode: 302, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Location": {"https://example.org/next"}}}, nil
	})
	_, err := client.Open(context.Background(), "https://example.org/start")
	if err == nil || apperror.Public(err).Code != "TooManyRedirects" || requests != 5 {
		t.Fatal("redirect limit changed", err, requests)
	}
}

func TestDialRejectsEveryUnsafeDNSAnswerBeforeConnecting(t *testing.T) {
	for _, tc := range []struct {
		name, address string
		ips           []string
		lookupErr     error
		code          string
	}{
		{"wrong-port", "example.org:80", nil, nil, "SourceDenied"},
		{"malformed", "example.org", nil, nil, "SourceDenied"},
		{"dns-failure", "example.org:443", nil, errors.New("dns secret"), "SourceDNS"},
		{"dns-empty", "example.org:443", nil, nil, "SourceDNS"},
		{"private-first", "example.org:443", []string{"127.0.0.1", "8.8.8.8"}, nil, "SourceDenied"},
		{"private-last", "example.org:443", []string{"8.8.8.8", "169.254.169.254"}, nil, "SourceDenied"},
		{"mapped-private", "example.org:443", []string{"::ffff:127.0.0.1"}, nil, "SourceDenied"},
		{"transition", "example.org:443", []string{"64:ff9b::a9fe:a9fe"}, nil, "SourceDenied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookups, dials := 0, 0
			_, err := dialPublic(context.Background(), "tcp", tc.address, func(context.Context, string) ([]net.IPAddr, error) {
				lookups++
				ips := []net.IPAddr{}
				for _, ip := range tc.ips {
					ips = append(ips, net.IPAddr{IP: net.ParseIP(ip)})
				}
				return ips, tc.lookupErr
			}, func(context.Context, string, string) (net.Conn, error) {
				dials++
				return nil, errors.New("unexpected connection")
			})
			if err == nil || apperror.Public(err).Code != tc.code || dials != 0 || tc.code == "SourceDenied" && tc.ips == nil && lookups != 0 {
				t.Fatal("unsafe DNS response reached dial", err, lookups, dials)
			}
		})
	}
}

func TestDialPinsValidatedIPsAndPreservesContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	addresses := []string{}
	conn, err := dialPublic(ctx, "tcp", "source.example:443", func(received context.Context, host string) ([]net.IPAddr, error) {
		if received != ctx || host != "source.example" {
			t.Error("DNS scope changed")
		}
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("2606:4700::1111")}}, nil
	}, func(received context.Context, network, address string) (net.Conn, error) {
		if received != ctx || network != "tcp" {
			t.Error("dial context changed")
		}
		addresses = append(addresses, address)
		if len(addresses) == 1 {
			return nil, errors.New("first address refused")
		}
		return left, nil
	})
	if err != nil || conn != left || !reflect.DeepEqual(addresses, []string{"8.8.8.8:443", "[2606:4700::1111]:443"}) {
		t.Fatal("validated addresses were not pinned", err, addresses)
	}
	cancel()
	_, err = dialPublic(ctx, "tcp", "source.example:443", func(received context.Context, _ string) ([]net.IPAddr, error) { return nil, received.Err() }, func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("cancelled lookup dialed")
		return nil, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled DNS error mapping changed", err)
	}
}

func TestOpenedHTTPBodyRemainsBoundToContext(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "4")
		w.Write([]byte("x"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	// Only this package's test can replace the guarded transport; production New
	// always installs the public-address dialer and exposes no HTTP client setter.
	wire := server.Client()
	wire.Transport.(*http.Transport).DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	client := &Client{http: wire}
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	file, err := client.Open(ctx, "https://127.0.0.1/file")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Body.Close()
	buf := make([]byte, 1)
	if n, err := file.Body.Read(buf); n != 1 || err != nil {
		t.Fatal("missing first byte", err)
	}
	done := make(chan error, 1)
	go func() { _, err := file.Body.Read(buf); done <- err }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled body read succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled body read remained blocked")
	}
}

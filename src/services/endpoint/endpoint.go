// Package endpoint validates S3 endpoints and builds transports protected against SSRF and DNS rebinding.
package endpoint

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

type Policy struct {
	Allowed   map[string]bool
	Overrides map[string]string
}

var reservedNetworks = [...]netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func Parse(value string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, apperror.New(400, "InvalidEndpoint", messages.EndpointInvalidEndpoint)
	}
	if u.Opaque != "" || strings.Contains(u.Hostname(), "%") {
		return nil, apperror.New(400, "InvalidEndpoint", messages.EndpointInvalidEndpointEndpoint)
	}
	u.Path = ""
	u.RawPath = ""
	u.Host = strings.ToLower(u.Host)
	return u, nil
}

func PublicIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
		return false
	}
	for _, block := range reservedNetworks {
		if block.Contains(addr) {
			return false
		}
	}
	return true
}

// Resolve and dial the checked IP itself. Recheck on every new connection so DNS
// rebinding cannot turn a public endpoint into a request to a private service.
func Transport(ctx context.Context, c Policy, public *url.URL) (*http.Transport, *url.URL, error) {
	trusted := c.Allowed[public.String()]
	target := public
	if value, ok := c.Overrides[public.String()]; ok {
		var err error
		target, err = Parse(value)
		if err != nil {
			return nil, nil, err
		}
		trusted = true
	}
	if !trusted && public.Scheme != "https" {
		return nil, nil, apperror.New(400, "EndpointNotAllowed", messages.EndpointEndpointNotAllowed)
	}
	resolve := func(ctx context.Context, host string) ([]net.IPAddr, error) {
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(ips) == 0 {
			return nil, apperror.NewWithCause(502, "EndpointUnavailable", messages.EndpointEndpointUnavailable, err)
		}
		if !trusted {
			for _, ip := range ips {
				if !PublicIP(ip.IP) {
					return nil, apperror.New(400, "EndpointNotAllowed", messages.EndpointEndpointNotAllowedEndpoint)
				}
			}
		}
		return ips, nil
	}
	if _, err := resolve(ctx, target.Hostname()); err != nil {
		return nil, nil, err
	}
	tr := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ForceAttemptHTTP2: true, MaxIdleConns: 8, MaxIdleConnsPerHost: 4, IdleConnTimeout: 60 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second, DisableCompression: true}
	tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(host, target.Hostname()) {
			return nil, errors.New("endpoint redirect denied")
		}
		expected := target.Port()
		if expected == "" {
			if target.Scheme == "https" {
				expected = "443"
			} else {
				expected = "80"
			}
		}
		if port != expected {
			return nil, errors.New("endpoint port redirect denied")
		}
		ips, err := resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		var last error
		for _, ip := range ips {
			conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
	return tr, target, nil
}

// Package remotesource adapts untrusted public HTTPS sources to the import port.
package remotesource

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	endpoints "github.com/owiks/golang-minio-s3-backend-api/src/services/endpoint"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads/source"
)

type Client struct{ http *http.Client }

func New() *Client       { return &Client{http: newHTTPClient()} }
func (c *Client) Close() { c.http.CloseIdleConnections() }

func (c *Client) Open(ctx context.Context, raw string) (source.File, error) {
	u, err := source.ParseURL(raw)
	if err != nil {
		return source.File{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return source.File{}, unavailable(ctx, err)
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.http.Do(req)
	if err != nil {
		return source.File{}, unavailable(ctx, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return source.File{}, apperror.New(502, "SourceResponse", messages.RemotesourceSourceResponse)
	}
	return source.File{Body: resp.Body, Size: resp.ContentLength, ContentType: resp.Header.Get("Content-Type")}, nil
}

func unavailable(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var safe *apperror.Error
	if errors.As(err, &safe) {
		return safe
	}
	return apperror.NewWithCause(502, "SourceUnavailable", messages.RemotesourceSourceUnavailable, err)
}

func PublicIP(ip net.IP) bool {
	if !endpoints.PublicIP(ip) {
		return false
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	for _, raw := range []string{"0.0.0.0/8", "240.0.0.0/4", "64:ff9b::/96", "64:ff9b:1::/48", "2002::/16", "2001::/32"} {
		if netip.MustParsePrefix(raw).Contains(addr) {
			return false
		}
	}
	return true
}

func dialPublic(ctx context.Context, network, address string, lookup func(context.Context, string) ([]net.IPAddr, error), dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port != "443" {
		return nil, apperror.New(400, "SourceDenied", messages.RemotesourceSourceDenied)
	}
	ips, err := lookup(ctx, host)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, apperror.NewWithCause(502, "SourceDNS", messages.RemotesourceSourceDNS, err)
	}
	if len(ips) == 0 {
		return nil, apperror.New(502, "SourceDNS", messages.RemotesourceSourceDNS)
	}
	for _, ip := range ips {
		if !PublicIP(ip.IP) {
			return nil, apperror.New(400, "SourceDenied", messages.RemotesourceSourceDeniedClient)
		}
	}
	var last error
	for _, ip := range ips {
		conn, e := dial(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if e == nil {
			return conn, nil
		}
		last = e
	}
	return nil, last
}

func newHTTPClient() *http.Client {
	tr := &http.Transport{Proxy: nil, DisableCompression: true, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second, IdleConnTimeout: time.Second}
	tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialPublic(ctx, network, address, net.DefaultResolver.LookupIPAddr, (&net.Dialer{Timeout: 10 * time.Second}).DialContext)
	}
	return &http.Client{Transport: tr, Timeout: 15 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		r.Header.Del("Referer")
		r.Header.Del("Authorization")
		r.Header.Del("Cookie")
		if len(via) >= 5 {
			return apperror.New(400, "TooManyRedirects", messages.RemotesourceTooManyRedirects)
		}
		_, e := source.ParseURL(r.URL.String())
		return e
	}}
}

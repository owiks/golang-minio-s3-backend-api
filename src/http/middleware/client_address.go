package middleware

import (
	"net"
	"net/http"
	"net/netip"
)

// ClientAddress ignores forwarding headers unless the immediate peer belongs
// to an explicitly trusted proxy network. Nginx must overwrite X-Real-IP.
func ClientAddress(r *http.Request, trustedProxies []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	addr = addr.Unmap()
	for _, prefix := range trustedProxies {
		if prefix.Contains(addr) {
			if real, err := netip.ParseAddr(r.Header.Get("X-Real-IP")); err == nil {
				return real.Unmap().String()
			}
			break
		}
	}
	return addr.String()
}

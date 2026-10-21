package middleware

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestProxyIdentityRequiresExplicitTrust(t *testing.T) {
	trustedProxies := []netip.Prefix{}
	r := httptest.NewRequest("POST", "http://ui/api/sessions", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("X-Real-IP", "198.51.100.2")
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := ClientAddress(r, trustedProxies); got != "192.0.2.1" {
		t.Fatal("spoofed identity", got)
	}
	trustedProxies = []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}
	if got := ClientAddress(r, trustedProxies); got != "198.51.100.2" {
		t.Fatal(got)
	}
	r.Header.Set("X-Real-IP", "invalid")
	if got := ClientAddress(r, trustedProxies); got != "192.0.2.1" {
		t.Fatal(got)
	}
}

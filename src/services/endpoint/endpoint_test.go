package endpoint

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOverrideTransportRejectsHostAndPortChanges(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer fixture.Close()
	public, _ := Parse("https://public.example")
	transport, target, err := Transport(context.Background(), Policy{Overrides: map[string]string{public.String(): fixture.URL}}, public)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport}).Get(target.String())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatal(response.StatusCode)
	}
	for _, address := range []string{"evil.example:9000", target.Hostname() + ":1"} {
		conn, err := transport.DialContext(context.Background(), "tcp", address)
		if conn != nil {
			conn.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "redirect denied") {
			t.Fatalf("changed endpoint accepted: %s, %v", address, err)
		}
	}
	if _, _, err := Transport(context.Background(), Policy{Overrides: map[string]string{public.String(): "file:///secret"}}, public); err == nil {
		t.Fatal("invalid override accepted")
	}
}

func TestEndpointValidation(t *testing.T) {
	for _, value := range []string{"file:///tmp", "https://user:secret@example.com", "https://example.com/path", "https://example.com?token=secret", "https://example.com#secret", "http://", "https://example.com?"} {
		if _, err := Parse(value); err == nil {
			t.Errorf("accepted invalid endpoint %q", value)
		}
	}
	u, err := Parse(" https://S3.Example.com/ ")
	if err != nil || u.String() != "https://s3.example.com" {
		t.Fatal(u, err)
	}
	for _, value := range []string{"127.0.0.1", "::1", "10.0.0.1", "172.16.0.1", "192.168.0.1", "169.254.169.254", "100.100.100.200", "224.0.0.1", "fc00::1", "2001:db8::1"} {
		if PublicIP(net.ParseIP(value)) {
			t.Errorf("private/reserved IP accepted: %s", value)
		}
	}
	if !PublicIP(net.ParseIP("1.1.1.1")) {
		t.Fatal("public IP rejected")
	}
	u, _ = Parse("https://127.0.0.1:19000")
	if _, _, err := Transport(context.Background(), Policy{}, u); err == nil {
		t.Fatal("private endpoint accepted without allowlist")
	}
}

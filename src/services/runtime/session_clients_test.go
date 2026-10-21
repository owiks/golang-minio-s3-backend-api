package runtime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
)

func TestConnectedSessionKeepsSigningAndRequestEndpointsSeparate(t *testing.T) {
	public, _ := url.Parse("https://public.example.test")
	internal, _ := url.Parse("http://internal.example.test:9000")
	tr := http.DefaultTransport.(*http.Transport).Clone()
	defer tr.CloseIdleConnections()
	before := time.Now()
	s, err := NewConnectedSession(Connection{AccessKey: "fixture-access", SecretKey: "fixture-secret", SessionToken: "fixture-token", Region: "us-east-1"}, config.Config{Origin: "https://app.example.test", SessionTTL: time.Hour, ConditionalDeleteEndpoints: map[string]bool{internal.String(): true}}, public, internal, tr)
	if err != nil {
		t.Fatal(err)
	}
	if s.transport != tr || s.s3.EndpointURL().Host != internal.Host || !s.conditionalDelete || !s.secureCookies || s.expires.Before(before.Add(time.Hour)) || s.id == "" || s.token == s.id || s.csrf == s.token {
		t.Fatal("session construction changed")
	}
	u, err := NewClients(s).PresignedGetObject(context.Background(), "test-bucket", "file.txt", time.Minute, nil)
	if err != nil || u.Host != public.Host || u.Scheme != "https" || u.Query().Get("X-Amz-Security-Token") != "fixture-token" {
		t.Fatal("public signing/session token lost", err)
	}
}

func TestListingPortPreservesNativeCursorAndCancellation(t *testing.T) {
	started := make(chan struct{})
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/test-bucket/" || q.Get("list-type") != "2" || q.Get("prefix") != "folder/" || q.Get("continuation-token") != "native+/=" || q.Get("delimiter") != "/" || q.Get("max-keys") != "200" || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Error("native listing scope/signature changed")
		}
		close(started)
		<-r.Context().Done()
	}))
	defer fixture.Close()
	endpoint, _ := url.Parse(fixture.URL)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	defer tr.CloseIdleConnections()
	s, err := NewConnectedSession(Connection{AccessKey: "fixture-access", SecretKey: "fixture-secret", Region: "us-east-1"}, config.Config{SessionTTL: time.Hour}, endpoint, endpoint, tr)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := NewClients(s).ListObjectPage(ctx, "test-bucket", "folder/", "native+/=", "/", 200)
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("listing did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("context lost", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listing did not stop")
	}
}

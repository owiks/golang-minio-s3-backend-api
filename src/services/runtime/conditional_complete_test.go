package runtime

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
)

func TestSessionConditionalCompleteRequiresExplicitEndpointAssertion(t *testing.T) {
	public, _ := url.Parse("https://public.example.test")
	internal, _ := url.Parse("http://internal.example.test:9000")
	for _, scenario := range []struct {
		name      string
		endpoints map[string]bool
		want      bool
	}{
		{"unknown", nil, false},
		{"public", map[string]bool{public.String(): true}, true},
		{"internal", map[string]bool{internal.String(): true}, true},
		{"false assertion", map[string]bool{public.String(): false}, false},
		{"other endpoint", map[string]bool{"https://other.example.test": true}, false},
		{"hostname is insufficient", map[string]bool{"public.example.test": true}, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			transport := http.DefaultTransport.(*http.Transport).Clone()
			defer transport.CloseIdleConnections()
			session, err := NewConnectedSession(Connection{AccessKey: "fixture-key", SecretKey: "fixture-secret", Region: "us-east-1"}, config.Config{SessionTTL: time.Hour, ConditionalCompleteEndpoints: scenario.endpoints}, public, internal, transport)
			if err != nil {
				t.Fatal(err)
			}
			client := &multipartClients{Clients: NewClients(session)}
			if client.ConditionalCompletionConfigured() != scenario.want {
				t.Fatal("incorrect conditional completion assertion", scenario.want)
			}
			if scenario.endpoints != nil {
				scenario.endpoints[public.String()] = !scenario.want
				scenario.endpoints[internal.String()] = !scenario.want
				if client.ConditionalCompletionConfigured() != scenario.want {
					t.Fatal("mutable configuration changed existing session guarantee")
				}
			}
		})
	}
}

func TestRuntimeConditionalCompleteConfigSnapshotIsDetached(t *testing.T) {
	c := config.Default()
	c.ConditionalCompleteEndpoints = map[string]bool{"https://s3.example": true}
	app := New(c)
	defer app.Close()
	c.ConditionalCompleteEndpoints["https://s3.example"] = false
	snapshot := app.Config()
	if !snapshot.ConditionalCompleteEndpoints["https://s3.example"] {
		t.Fatal("runtime policy aliases original configuration")
	}
	snapshot.ConditionalCompleteEndpoints["https://s3.example"] = false
	if !app.Config().ConditionalCompleteEndpoints["https://s3.example"] {
		t.Fatal("runtime policy escaped through snapshot")
	}
}

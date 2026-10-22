package runtime

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestServiceClientSignsPublicEndpoint(t *testing.T) {
	create := func(endpoint string) *minio.Client {
		t.Helper()
		client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4("fixture-access", "fixture-secret", ""), Region: "us-east-1", Secure: true})
		if err != nil {
			t.Fatal(err)
		}
		return client
	}
	s := &Session{id: "private-session", sessionCore: &sessionCore{actor: "private-actor", s3: create("internal.example.test:9000"), signer: create("public.example.test")}}
	client := NewClients(s)
	link, err := client.PresignedGetObject(context.Background(), "test-bucket", "folder/file.txt", time.Minute, url.Values{"response-content-type": {"text/plain"}})
	if err != nil {
		t.Fatal(err)
	}
	if link.Host != "public.example.test" || link.Scheme != "https" || link.Path != "/test-bucket/folder/file.txt" || link.Query().Get("response-content-type") != "text/plain" {
		t.Fatal("signing must use the public endpoint and preserve response parameters")
	}
	// Neither embedded SDK clients nor authenticated session values may reach JSON.
	raw, err := json.Marshal(client)
	if err != nil || string(raw) != "{}" {
		t.Fatalf("adapter serialization: %s %v", raw, err)
	}
}

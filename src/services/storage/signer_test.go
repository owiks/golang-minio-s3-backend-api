package storage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/minio-go/v7/pkg/credentials"
	"minio-web/api/src/services/apperror"
)

func TestSignerRequiresExplicitRegionAndNeverFetchesPublicLocation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "public endpoint must not be called", 500)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	config := Config{Endpoint: u, Credentials: credentials.NewStaticV4("fixture-access", "fixture-secret", ""), Region: "us-east-1"}
	client, err := NewSigner(config)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := client.PresignedGetObject(context.Background(), "fixture-bucket", "file.csv", time.Minute, nil)
	if err != nil || signed.Host != u.Host || signed.Query().Get("X-Amz-Signature") == "" || calls.Load() != 0 {
		t.Fatal(signed, err, calls.Load())
	}
	config.Region = ""
	if client, err := NewSigner(config); client != nil || apperror.Public(err).Code != "SigningRegionRequired" || calls.Load() != 0 {
		t.Fatal(client, err, calls.Load())
	}
	config.Region = " "
	if client, err := NewSigner(config); client != nil || apperror.Public(err).Code != "SigningRegionRequired" || calls.Load() != 0 {
		t.Fatal(client, err, calls.Load())
	}
}

func TestSignerTransportFailsClosedForSDKNetworkMethod(t *testing.T) {
	u, _ := url.Parse("https://never-dial.fixture.invalid")
	client, err := NewSigner(Config{Endpoint: u, Credentials: credentials.NewStaticV4("fixture-access", "fixture-secret", ""), Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListBuckets(context.Background())
	if apperror.Public(err).Code != "SigningNetworkDisabled" {
		t.Fatal("signer attempted or permitted network", err)
	}
}

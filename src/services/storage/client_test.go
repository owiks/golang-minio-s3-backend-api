package storage

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCoreListingUsesRequestCancellationAndPathStyle(t *testing.T) {
	u, _ := url.Parse("http://fixture.example:9000")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "fixture.example:9000" || r.URL.Path != "/bucket/" || r.URL.Query().Get("prefix") != "a +%/" || r.Header.Get("Authorization") == "" {
			t.Errorf("SDK request configuration changed: host=%s path=%s", r.URL.Host, r.URL.Path)
		}
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	client, err := NewS3(Config{Endpoint: u, Credentials: credentials.NewStaticV4("test", "test-secret", ""), Region: "us-east-1"}, WithContext(ctx, transport))
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		_, err := (minio.Core{Client: client}).ListObjectsV2("bucket", "a +%/", "", "", "/", 1)
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Core.ListObjectsV2 did not stop on cancellation")
	}
}

package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"minio-web/api/src/config"
)

func TestConnectedSessionNormalizesRegionAndSignsPublicURLWithoutNetwork(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "unexpected upstream request", 500)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	internal, _ := url.Parse("http://internal.fixture.invalid:9000")
	for _, region := range []string{"", " ", "us-east-1"} {
		s, err := NewConnectedSession(Connection{AccessKey: "fixture-access", SecretKey: "fixture-secret", Region: region}, config.Config{}.WithDefaults(), u, internal, &http.Transport{})
		if err != nil {
			t.Fatal(err)
		}
		signed, err := NewClients(s).PresignedGetObject(context.Background(), "fixture-bucket", "file", time.Minute, nil)
		if err != nil || signed.Host != u.Host || s.region != "us-east-1" || calls.Load() != 0 {
			t.Fatal(signed, err, s.region, calls.Load())
		}
		closeSession(s)
	}
}

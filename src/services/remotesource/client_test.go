package remotesource

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/staging"
	uploadssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/uploads"
)

type remoteTestTransport func(*http.Request) (*http.Response, error)

func (f remoteTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRemoteStagingAndLimits(t *testing.T) {
	for _, test := range []struct {
		Status int    `json:"-"`
		Size   int64  `json:"-"`
		Body   string `json:"-"`
		Ok     bool   `json:"-"`
	}{{200, 4, "text", true}, {403, 4, "text", false}, {200, 20, "text", false}, {200, uploadssvc.MaxRemoteSize + 1, "", false}, {200, -1, "text", true}} {
		client := &Client{http: &http.Client{Transport: remoteTestTransport(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				t.Fatal("forwarded credentials")
			}
			return &http.Response{StatusCode: test.Status, ContentLength: test.Size, Body: io.NopCloser(strings.NewReader(test.Body)), Header: http.Header{"Content-Type": []string{"text/plain"}}}, nil
		})}}
		budget := staging.New(uploadssvc.MaxRemoteSize)
		defer budget.Close()
		f, n, _, err := uploadssvc.StageRemote(context.Background(), client, "https://example.org/a", func(int64, int64) {}, budget)
		if (err == nil) != test.Ok {
			t.Fatalf("%+v: %v", test, err)
		}
		if f != nil {
			if n != 4 {
				t.Fatal(n)
			}
			name := f.Name()
			f.Close()
			os.Remove(name)
		}
	}
}

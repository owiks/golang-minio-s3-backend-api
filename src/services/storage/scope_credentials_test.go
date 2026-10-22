package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
)

func TestMultipartCleanupCannotBypassNativeCredentialVersion(t *testing.T) {
	for _, current := range []bool{false, true} {
		t.Run(map[bool]string{false: "replaced", true: "unchanged"}[current], func(t *testing.T) {
			calls := 0
			wire := ScopeTransport{
				Base: scopeWire(func(*http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader(""))}, nil
				}),
				Policy: authstate.FullPolicy(),
				Load: func(context.Context) (authstate.Policy, error) {
					return authstate.Policy{}, authstate.Denied()
				},
				CheckCleanup: func(context.Context) error {
					if !current {
						return authstate.Denied()
					}
					return nil
				},
			}
			response, err := wire.RoundTrip(httptest.NewRequest("DELETE", "http://s3.test/data/file?uploadId=ours", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if current && (response.StatusCode != http.StatusNoContent || calls != 1) || !current && (response.StatusCode != http.StatusForbidden || calls != 0) {
				t.Fatal("cleanup did not respect credential version", response.StatusCode, calls)
			}
		})
	}
}

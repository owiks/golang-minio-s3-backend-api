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

type scopeWire func(*http.Request) (*http.Response, error)

func (f scopeWire) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestScopeTransportChecksActualResources(t *testing.T) {
	p := authstate.Policy{Scopes: []string{"objects:read", "objects:write", "objects:delete", "buckets:read"}, Resources: []authstate.Resource{{Bucket: "data", Prefix: "reports/"}}}
	for _, tc := range []struct {
		method, path, source, body string
		allow                      bool
	}{
		{"GET", "/data/reports/file", "", "", true},
		{"GET", "/data/reports%2Ffile", "", "", true},
		{"GET", "/data/reports%252Ffile", "", "", false},
		{"GET", "/data/outside/file", "", "", false},
		{"GET", "/other/reports/file", "", "", false},
		{"GET", "/data?list-type=2&prefix=reports%2F", "", "", true},
		{"GET", "/data?list-type=2&prefix=", "", "", false},
		{"GET", "/data?versions&prefix=reports%2F", "", "", true},
		{"GET", "/data?location", "", "", true},
		{"GET", "/data?policy", "", "", false},
		{"PUT", "/data/reports/copied", "/data/outside/file", "", false},
		{"PUT", "/data/reports/copied", "/data/reports/file", "", true},
		{"PUT", "/data/reports/copied", "/data/reports%2Ffile?versionId=opaque", "", true},
		{"POST", "/data?delete", "", "<Delete><Object><Key>reports/file</Key></Object></Delete>", true},
		{"POST", "/data?delete", "", "<Delete><Object><Key>reports/file</Key></Object><Object><Key>outside/file</Key></Object></Delete>", false},
		{"POST", "/data?delete", "", "<Error><Object><Key>reports/file</Key></Object></Error>", false},
	} {
		t.Run(tc.method+tc.path+tc.source+tc.body, func(t *testing.T) {
			calls := 0
			wire := scopeWire(func(r *http.Request) (*http.Response, error) {
				calls++
				if tc.body != "" {
					raw, _ := io.ReadAll(r.Body)
					if string(raw) != tc.body {
						t.Fatal("signed bytes changed")
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
			})
			r := httptest.NewRequest(tc.method, "http://s3.test"+tc.path, strings.NewReader(tc.body))
			r.Header.Set("X-Amz-Copy-Source", tc.source)
			res, err := (ScopeTransport{Base: wire, Policy: p}).RoundTrip(r)
			if res != nil {
				_ = res.Body.Close()
			}
			if (err == nil && res.StatusCode == 200) != tc.allow || calls != boolInt(tc.allow) {
				t.Fatalf("allow=%v upstream calls=%d err=%v", tc.allow, calls, err)
			}
		})
	}
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func TestRevocationBlocksEvenFullPolicyAndAllowsOnlyAbortCleanup(t *testing.T) {
	calls := 0
	tpt := ScopeTransport{Base: scopeWire(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
	}), Policy: authstate.FullPolicy(), Load: func(context.Context) (authstate.Policy, error) { return authstate.Policy{}, authstate.Denied() }}
	for _, tc := range []struct {
		method, path string
		allow        bool
	}{{"GET", "/data/reports/file", false}, {"DELETE", "/data/reports/file", false}, {"DELETE", "/data/reports/file?uploadId=ours", true}} {
		res, err := tpt.RoundTrip(httptest.NewRequest(tc.method, "http://s3.test"+tc.path, nil))
		if res != nil {
			_ = res.Body.Close()
		}
		if (err == nil && res.StatusCode == 200) != tc.allow {
			t.Fatal(tc, err)
		}
		if err != nil || !tc.allow && res.StatusCode != 403 {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("revoked request reached upstream")
	}
}

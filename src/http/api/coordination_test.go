package api

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
)

func matchedOperation(t *testing.T, method, path string) routes.Match {
	t.Helper()
	match := registry.Match(method, "/api/sessions/private/"+path)
	if !match.MethodAllowed {
		t.Fatalf("missing operation: %s %s", method, path)
	}
	return match
}
func TestResourceLocksPreserveDependencies(t *testing.T) {
	query := url.Values{"bucket": {"images"}}
	for _, tc := range []struct{ method, path string }{
		{"POST", "admin/users"}, {"PUT", "admin/users/alice/status"}, {"POST", "admin/groups/team/members"}, {"PUT", "admin/policies/viewer"}, {"POST", "admin/bindings"}, {"PUT", "admin/access-keys/key/status"},
	} {
		m := matchedOperation(t, tc.method, tc.path)
		if !m.NeedsMutationLock() || m.MutationResource(query) != "iam" {
			t.Fatal(tc)
		}
	}
	tags := matchedOperation(t, "PUT", "admin/buckets/images/tags")
	deletion := matchedOperation(t, "DELETE", "admin/object-delete")
	if tags.MutationResource(query) != deletion.MutationResource(query) {
		t.Fatal("bucket dependencies split")
	}
	other := matchedOperation(t, "PUT", "admin/buckets/other/tags")
	if other.MutationResource(query) == tags.MutationResource(query) {
		t.Fatal("independent buckets share lock")
	}
}
func TestOperationLabelsNeverIncludeResourceIdentifiers(t *testing.T) {
	for _, path := range []string{"/api/sessions/PRIVATE-ID/admin/users/PRIVATE-USER/status?secret=PRIVATE-TOKEN", "/api/shared-folder/file/PRIVATE-TICKET", "/api/sessions/PRIVATE-ID/unknown/PRIVATE-NAME", "/unknown/PRIVATE-NAME"} {
		r := httptest.NewRequest("PUT", "http://ui"+path, nil)
		if got := operationName(r); strings.Contains(got, "PRIVATE") {
			t.Fatal("unsafe metric/log label", got)
		}
	}
	r := httptest.NewRequest("PUT", "http://ui/api/sessions/secret/admin/users/alice/status", nil)
	if got := operationName(r); got != "PUT /api/sessions/{sessionID}/admin/users/{name}/status" {
		t.Fatal(got)
	}
}
func TestBodyNamedResourcesAcquireOnlyAfterDecode(t *testing.T) {
	for _, path := range []string{"admin/buckets", "objects/folders"} {
		m := matchedOperation(t, "POST", path)
		if m.Operation.Mutation != routes.AfterDecode || m.NeedsMutationLock() {
			t.Fatal("resource must be locked after decode", path)
		}
	}
	for _, tc := range []struct{ method, path string }{{"PUT", "admin/buckets/images/tags"}, {"POST", "admin/users"}} {
		if !matchedOperation(t, tc.method, tc.path).NeedsMutationLock() {
			t.Fatal("dependency lock skipped", tc)
		}
	}
	if matchedOperation(t, "GET", "admin/buckets").NeedsMutationLock() {
		t.Fatal("read acquires mutation lock")
	}
}

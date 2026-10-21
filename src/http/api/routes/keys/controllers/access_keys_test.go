package controllers_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/keys"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

func TestKeyCommandHTTPPreflight(t *testing.T) {
	for _, tc := range []struct {
		name, method, kind, key, action, body, code string
	}{
		{"edit-current-before-decode", "PUT", "access-keys", "actor", "", "{", "CurrentAccessKey"},
		{"status-current-before-decode", "PUT", "access-keys", "actor", "status", "{", "CurrentAccessKey"},
		{"rotate-current-before-decode", "POST", "access-keys", "actor", "rotate", "{", "CurrentAccessKey"},
		{"edit-invalid-name", "PUT", "access-keys", "target", "", `{"name":"bad name","policy":null,"revision":"r"}`, "InvalidKeyName"},
		{"status-missing-value", "PUT", "access-keys", "target", "status", `{"revision":"r"}`, "InvalidStatus"},
		{"rotate-wrong-confirmation", "POST", "access-keys", "target", "rotate", `{"confirmation":"wrong","revision":"r"}`, "ConfirmationRequired"},
		{"edit-invalid-json", "PUT", "access-keys", "target", "", "{", "InvalidJSON"},
		{"status-invalid-json", "PUT", "access-keys", "target", "status", "{", "InvalidJSON"},
		{"rotate-invalid-json", "POST", "access-keys", "target", "rotate", "{", "InvalidJSON"},
		{"temporary-read-only", "PUT", "temporary-access", "actor", "", "{", "MethodNotAllowed"},
		{"edit-wrong-method", "PATCH", "access-keys", "target", "", "{", "MethodNotAllowed"},
		{"status-wrong-method", "POST", "access-keys", "target", "status", "{", "MethodNotAllowed"},
		{"rotate-wrong-method", "PUT", "access-keys", "target", "rotate", "{", "MethodNotAllowed"},
		{"unknown-action", "PUT", "access-keys", "target", "unknown", "{", "MethodNotAllowed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// No SDK client or transport: any upstream access is a test failure.
			s := testsession.With(t, runtime.Connection{AccessKey: "actor"}, config.Config{}, nil)
			r := httptest.NewRequest(tc.method, "/", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			registry, err := routes.NewRegistry(keys.GetRoutes())
			if err != nil {
				t.Fatal(err)
			}
			path := "/api/sessions/" + s.SessionID() + "/admin/" + tc.kind + "/" + tc.key
			if tc.action != "" {
				path += "/" + tc.action
			}
			match := registry.Match(tc.method, path)
			if !match.MethodAllowed {
				err = apperror.New(405, "MethodNotAllowed", messages.Literal("Метод не поддерживается."))
			} else {
				err = match.Operation.Handler(nil, httptest.NewRecorder(), r, s, match.Rest)
			}
			if err == nil || apperror.Public(err).Code != tc.code {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
		})
	}
}

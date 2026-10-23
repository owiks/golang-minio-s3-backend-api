package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/openapi"
	authcontrollers "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/authentication/controllers"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operationpolicy"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

func TestCurrentCredentialReportsOnlySafeEffectiveOperationRestrictions(t *testing.T) {
	for _, policy := range []operationpolicy.Policy{{}, {ReadOnly: true, DisableUploads: true, DisableDeletes: true, DisableAdminChanges: true}} {
		cfg := config.Default()
		cfg.Operations = policy
		cfg.TokenTTL = 2 * time.Hour
		cfg.OperatorToken = "operator-secret-must-never-enter-a-session-response"
		app := runtime.New(cfg)
		t.Cleanup(app.Close)
		s := testsession.New(t, "http://fixture.example")
		if err := app.StoreSession(s); err != nil {
			t.Fatal(err)
		}
		for _, language := range []string{"en", "ru"} {
			r := operatorRequest(s, "GET", "credential", "")
			r.Header.Set("Accept-Language", language)
			w := httptest.NewRecorder()
			ServeHTTP(app, w, r)
			var got authcontrollers.CurrentView
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.ID != s.SessionID() || got.ConnectionID == "" {
				t.Fatal("current credential contract changed", w.Code, w.Body.String())
			}
			want := authcontrollers.EffectiveOperationsView{ReadOnly: policy.ReadOnly, DisableUploads: policy.DisableUploads, DisableDeletes: policy.DisableDeletes, DisableAdminChanges: policy.DisableAdminChanges}
			if got.EffectiveOperations != want {
				t.Fatal("effective operations changed with language or maintenance", language, got.EffectiveOperations, want)
			}
			if got.MaxTokenTTLSeconds != 7200 {
				t.Fatal("token TTL limit changed with language or maintenance", language, got.MaxTokenTTLSeconds)
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
				t.Fatal(err)
			}
			if raw["AccessView"] != nil || raw["id"] == nil || raw["policy"] == nil || raw["effectiveOperations"] == nil || raw["maxTokenTTLSeconds"] == nil {
				t.Fatal("additive fields must retain the flat credential contract", w.Body.String())
			}
			for _, hidden := range []string{cfg.OperatorToken, "operatorToken", "cookieToken", "secretKey", "digest", "previousDigest"} {
				if strings.Contains(w.Body.String(), hidden) {
					t.Fatal("current credential exposed private settings", hidden)
				}
			}
		}
	}
	properties := openapi.TypeSchema(authcontrollers.CurrentView{}, false).Value.Properties
	for _, field := range []string{"id", "connectionID", "policy", "effectiveOperations", "maxTokenTTLSeconds"} {
		if properties[field] == nil {
			t.Fatal("current credential response missing from OpenAPI", field)
		}
	}
	if !properties["maxTokenTTLSeconds"].Value.Type.Is("integer") {
		t.Fatal("token TTL limit must remain an integer in OpenAPI")
	}
	for _, field := range []string{"readOnly", "disableUploads", "disableDeletes", "disableAdminChanges"} {
		if properties["effectiveOperations"].Value.Properties[field] == nil {
			t.Fatal("effective restriction missing from OpenAPI", field)
		}
	}
}

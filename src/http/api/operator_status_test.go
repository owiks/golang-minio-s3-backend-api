package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/doc"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	operatorroutes "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/operator"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
)

func TestOperatorStatusDisabledByDefault(t *testing.T) {
	app := New(config.Default())
	defer app.Close()
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/api/operator/operations", nil)
		r.Header.Set("Authorization", "Bearer "+strings.Repeat("secret", 8))
		app.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "queue") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestOperatorStatusUsesIndependentCredentialAndSafeAggregate(t *testing.T) {
	cfg := config.Default()
	cfg.OperatorToken = strings.Repeat("operator-secret-", 4)
	cfg.Operations.ReadOnly = true
	f := newOperationsFixture(t, cfg, authstate.FullPolicy())
	created := operationView(t, f.call("POST", "/scans", `{"bucket":"data","prefix":"reports/"}`, http.StatusAccepted))
	call := func(path, language string, headers []string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(&http.Cookie{Name: "minio_session_" + f.token.ID, Value: f.token.Token})
		r.Header.Set("Accept-Language", language)
		for _, value := range headers {
			r.Header.Add("Authorization", value)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	for _, language := range []string{"en", "ru"} {
		for _, headers := range [][]string{nil, {"Bearer " + f.token.Token}, {"Bearer " + cfg.OperatorToken, "Bearer " + cfg.OperatorToken}, {"Bearer  " + cfg.OperatorToken}} {
			w := call("/api/operator/operations", language, headers)
			if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "InvalidOperatorToken") || w.Header().Get("WWW-Authenticate") == "" || strings.Contains(w.Body.String(), "operator.authentication") {
				t.Fatal(w.Code, w.Body.String())
			}
		}
	}
	w := call("/api/operator/operations", "en", []string{"Bearer " + cfg.OperatorToken})
	var got operations.OperatorStatus
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil || !operations.ValidStatus(got) || got.Queue.Queued != 1 || got.Worker.Scope != "process" || got.Worker.Running || !got.Policy.ReadOnly {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, private := range []string{cfg.OperatorToken, f.token.ID, f.connection.Snapshot().ID, created.ID, "reports/", "data", "leaseID", "manifest"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("operator summary leaked private data", private)
		}
	}
	w = call("/api/operator/operations?token="+cfg.OperatorToken, "en", []string{"Bearer " + cfg.OperatorToken})
	if w.Code != http.StatusBadRequest || strings.Contains(w.Body.String(), cfg.OperatorToken) {
		t.Fatal("query secret accepted or leaked", w.Code)
	}
	if err := f.app.Operations.Close(); err != nil {
		t.Fatal(err)
	}
	if w = call("/api/operator/operations", "en", []string{"Bearer " + cfg.OperatorToken}); w.Code != http.StatusServiceUnavailable {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestOperatorRouteSecurityIsExplicitAndDocumented(t *testing.T) {
	registered := operatorroutes.GetRoutes()
	registered[0].Operations[0].Operator = false
	if _, err := routes.NewRegistry(registered); err == nil {
		t.Fatal("operator namespace lost independent auth")
	}
	registered = operatorroutes.GetRoutes()
	registered[0].Path = "/health"
	if _, err := routes.NewRegistry(registered); err == nil {
		t.Fatal("operator auth used outside operator namespace")
	}
	document := doc.Build(Catalog())
	op := document.Paths.Value("/api/operator/operations").Get
	if op.Security == nil || len(*op.Security) != 1 || (*op.Security)[0]["OperatorBearer"] == nil {
		t.Fatal("operator bearer security missing")
	}
	if _, ok := (*op.Security)[0]["SessionBearer"]; ok {
		t.Fatal("user bearer can access operator status")
	}
}

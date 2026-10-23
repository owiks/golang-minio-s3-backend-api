package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func TestNonexpiringTokenHTTPContract(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("access lifecycle unexpectedly contacted native storage")
		w.WriteHeader(500)
	}))
	defer upstream.Close()
	cfg := config.Default()
	cfg.Endpoints.Allowed[upstream.URL] = true
	app := runtime.New(cfg)
	defer app.Close()
	const rootID, rootSecret, cid = "permanent-root", "permanent-root-secret", "permanent-connection"
	if err := app.Auth.Bootstrap(context.Background(), authstate.Bootstrap{ID: rootID, ConnectionID: cid, Endpoint: upstream.URL, Mode: "bearer", Digest: authstate.Digest(rootSecret), Secrets: authstate.Secrets{AccessKey: "fixture", SecretKey: "fixture-secret"}}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(app)
	call := func(method, actor, token, path, body string, want int) []byte {
		t.Helper()
		r := httptest.NewRequest(method, "http://api.test/api/sessions/"+actor+"/"+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, want, w.Body.String())
		}
		return w.Body.Bytes()
	}
	issuePath := "connections/" + cid + "/tokens"
	for _, body := range []string{`{"policy":{"scopes":["objects:read"]}}`, `{"ttlSeconds":null,"policy":{"scopes":["objects:read"]}}`, `{"ttlSeconds":-1,"policy":{"scopes":["objects:read"]}}`, `{"ttlSeconds":3601,"policy":{"scopes":["objects:read"]}}`} {
		call("POST", rootID, rootSecret, issuePath, body, 400)
	}
	var issued authstate.Issued
	if err := json.Unmarshal(call("POST", rootID, rootSecret, issuePath, `{"ttlSeconds":0,"policy":{"scopes":["objects:read"]}}`, 201), &issued); err != nil || !issued.ExpiresAt.IsZero() || issued.ExpiresIn != 0 || issued.Token == "" {
		t.Fatal("permanent wire response was not explicit", issued.ExpiresAt, issued.ExpiresIn, err)
	}
	var current authstate.AccessView
	if err := json.Unmarshal(call("GET", issued.ID, issued.Token, "credential", "", 200), &current); err != nil || !current.ExpiresAt.IsZero() || !current.Active {
		t.Fatal("permanent credential could not authenticate", err)
	}
	renewPath := "tokens/" + issued.ID + "/renew"
	for _, body := range []string{`{"revision":1}`, `{"revision":1,"ttlSeconds":null}`} {
		call("POST", issued.ID, issued.Token, renewPath, body, 400)
	}
	if err := json.Unmarshal(call("POST", issued.ID, issued.Token, renewPath, `{"revision":1,"ttlSeconds":0}`, 200), &current); err != nil || !current.ExpiresAt.IsZero() || current.Revision != 2 {
		t.Fatal("permanent self-renewal failed", err)
	}
	var rotated authstate.Issued
	if err := json.Unmarshal(call("POST", rootID, rootSecret, "tokens/"+issued.ID+"/rotate", `{"revision":2,"graceSeconds":0}`, 200), &rotated); err != nil || !rotated.ExpiresAt.IsZero() || rotated.ExpiresIn != 0 || rotated.Token == issued.Token {
		t.Fatal("permanent rotation failed", err)
	}
	call("GET", issued.ID, issued.Token, "credential", "", 401)
	call("GET", issued.ID, rotated.Token, "credential", "", 200)
	call("DELETE", rootID, rootSecret, "tokens/"+issued.ID, `{"revision":3}`, 200)
	call("GET", issued.ID, rotated.Token, "credential", "", 401)
	saved, _, err := app.Auth.Lookup(context.Background(), issued.ID)
	if err != nil || saved.Valid(time.Now()) {
		t.Fatal("permanent grant survived revocation", err)
	}
}

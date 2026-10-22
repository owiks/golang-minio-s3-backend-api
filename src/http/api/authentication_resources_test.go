package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/archives/plan"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/folders/share"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

func TestNarrowedAccessCannotReadCachedPlansOrRevokedDelegations(t *testing.T) {
	var native atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { native.Add(1) }))
	defer upstream.Close()
	cfg := config.Default()
	cfg.Endpoints.Allowed[upstream.URL] = true
	app := runtime.New(cfg)
	defer app.Close()
	ctx := context.Background()
	root := testsession.New(t, upstream.URL)
	if err := app.StoreSession(root); err != nil {
		t.Fatal(err)
	}
	a, c, err := app.Auth.Lookup(ctx, root.SessionID())
	if err != nil {
		t.Fatal(err)
	}
	g := authstate.CredentialGuard(a, false)
	child, err := app.Auth.Issue(ctx, g, c.Snapshot().ID, authstate.IssueRequest{Name: "reader", TTLSeconds: 600, Policy: authstate.Policy{Scopes: []string{"objects:read"}, Resources: []authstate.Resource{{Bucket: "data"}}}})
	if err != nil {
		t.Fatal(err)
	}
	cached := archiveplan.New(archiveplan.Config{SessionID: child.ID, Bucket: "data", Name: "files.zip", Created: time.Now(), Items: []archiveplan.ArchiveItem{{Key: "outside/private.txt"}}})
	ticket, err := app.Archives.Put(ctx, cached)
	if err != nil {
		t.Fatal(err)
	}
	grant := foldershare.New(foldershare.Config{Owner: child.ID, Token: "separate-share-secret", Bucket: "data", Prefix: "reports/", Expires: time.Now().Add(time.Minute)})
	if err = app.Shares.Add(grant); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(app)
	call := func(path, token string, want int) {
		t.Helper()
		r := httptest.NewRequest("GET", "http://api.test"+path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	path := "/api/sessions/" + child.ID + "/objects/archive-status/" + ticket
	call(path, child.Token, 200)
	policy := authstate.Policy{Scopes: []string{"objects:read"}, Resources: []authstate.Resource{{Bucket: "data", Prefix: "reports/"}}}
	if _, err = app.Auth.Edit(ctx, g, child.ID, authstate.EditRequest{Revision: 1, Policy: &policy}); err != nil {
		t.Fatal(err)
	}
	call(path, child.Token, 403)
	call("/api/shared-folder?mode=info", grant.Token(), 200)
	if err = app.Auth.Revoke(ctx, g, child.ID, 2); err != nil {
		t.Fatal(err)
	}
	call("/api/shared-folder?mode=info", grant.Token(), 403)
	if native.Load() != 0 {
		t.Fatal("metadata authorization contacted native storage")
	}
}

func TestPreviousCredentialCannotAdministerOrIssueLongLivedLinks(t *testing.T) {
	var native atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { native.Add(1) }))
	defer upstream.Close()
	cfg := config.Default()
	cfg.Endpoints.Allowed[upstream.URL] = true
	app := runtime.New(cfg)
	defer app.Close()
	ctx := context.Background()
	root := testsession.New(t, upstream.URL)
	if err := app.StoreSession(root); err != nil {
		t.Fatal(err)
	}
	a, c, err := app.Auth.Lookup(ctx, root.SessionID())
	if err != nil {
		t.Fatal(err)
	}
	g := authstate.CredentialGuard(a, false)
	child, err := app.Auth.Issue(ctx, g, c.Snapshot().ID, authstate.IssueRequest{TTLSeconds: 600, Policy: authstate.FullPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	next, err := app.Auth.Rotate(ctx, g, child.ID, authstate.RotateRequest{Revision: 1, GraceSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(app)
	base := "/api/sessions/" + child.ID
	call := func(method, path, token, body string, want int) []byte {
		t.Helper()
		r := httptest.NewRequest(method, "http://api.test"+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatal(method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	call("GET", base+"/credential", child.Token, "", 200)
	call("GET", base+"/connections", child.Token, "", 403)
	call("POST", base+"/admin/users", child.Token, `{}`, 403)
	call("PUT", base+"/admin/buckets/data/policy", child.Token, `{}`, 403)
	call("POST", base+"/tokens/"+child.ID+"/rotate", child.Token, `{"revision":2}`, 403)
	call("DELETE", base, child.Token, "", 403)
	call("GET", base+"/connections", next.Token, "", 200)
	linkPath := base + "/objects/url?bucket=data&key=reports%2Ffile.txt&mode=share&expiresIn=3600"
	var oldLink, newLink struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	if err = json.Unmarshal(call("GET", linkPath, child.Token, "", 200), &oldLink); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(call("GET", linkPath, next.Token, "", 200), &newLink); err != nil {
		t.Fatal(err)
	}
	expiry := func(raw string) (int64, time.Time) {
		t.Helper()
		u, e := url.Parse(raw)
		if e != nil {
			t.Fatal(e)
		}
		seconds, e := strconv.ParseInt(u.Query().Get("X-Amz-Expires"), 10, 64)
		if e != nil {
			t.Fatal(e)
		}
		date, e := time.Parse("20060102T150405Z", u.Query().Get("X-Amz-Date"))
		if e != nil {
			t.Fatal(e)
		}
		return seconds, date.Add(time.Duration(seconds) * time.Second)
	}
	short, shortExpiry := expiry(oldLink.URL)
	long, longExpiry := expiry(newLink.URL)
	if short < 1 || short > 60 || long <= 60 || long > 600 || !oldLink.ExpiresAt.Equal(shortExpiry) || !newLink.ExpiresAt.Equal(longExpiry) {
		t.Fatal("signature or advertised expiry ignores credential version")
	}
	if _, err = app.Auth.Rotate(ctx, g, child.ID, authstate.RotateRequest{Revision: 2, GraceSeconds: 0}); err != nil {
		t.Fatal(err)
	}
	call("GET", base+"/credential", child.Token, "", 401)
	call("GET", base+"/credential", next.Token, "", 401)
	if native.Load() != 0 {
		t.Fatal("denied administration contacted upstream")
	}
}

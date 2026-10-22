package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
	apihttp "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence"
	"github.com/owiks/golang-minio-s3-backend-api/src/persistence/secretbox"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/tests/databasefixtures"
	"github.com/owiks/golang-minio-s3-backend-api/tests/statefixtures"
)

func TestPersistentAccessNativeIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("requires isolated MinIO fixture")
	}
	_, client, bucket := fileFixture(t)
	for _, key := range []string{"reports/file.txt", "outside/file.txt"} {
		if _, err := client.PutObject(context.Background(), bucket, key, strings.NewReader("content"), 7, minio.PutObjectOptions{ContentType: "text/plain"}); err != nil {
			t.Fatal(err)
		}
	}
	drivers := []string{"sqlite"}
	if os.Getenv("STATE_INTEGRATION") == "1" {
		drivers = append(drivers, "postgres", "mysql")
	}
	for _, driver := range drivers {
		t.Run(driver, func(t *testing.T) {
			cfg := testConfig()
			cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
			cfg.State = databasefixtures.Database(t, driver)
			statefixtures.Migrate(t, cfg.State)
			ring, err := secretbox.Generate()
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(ring)
			cfg.State.KeyringFile = filepath.Join(t.TempDir(), "keyring.json")
			if err = os.WriteFile(cfg.State.KeyringFile, raw, 0600); err != nil {
				t.Fatal(err)
			}
			var app *runtime.State
			var srv *httptest.Server
			start := func() {
				m, e := persistence.Open(context.Background(), cfg)
				if e != nil {
					t.Fatal(e)
				}
				app = runtime.NewWithAuth(cfg, m)
				srv = httptest.NewServer(apihttp.NewHandler(app))
			}
			stop := func() { srv.Close(); app.Close() }
			start()
			defer func() { stop() }()
			checkRenewableAccessHTTP(t, srv.URL)
			checkDelegatedAccessHTTP(t, srv.URL, bucket)
			root := issueTestToken(t, srv.URL, "minio-web-test", "minio-web-test-secret", nil)
			cookie := loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret")
			base := "/api/sessions/" + root.ID
			var self authstate.AccessView
			if err = json.Unmarshal(tokenRequest(t, srv.URL, "GET", base+"/credential", root.Token, nil, nil, 200), &self); err != nil {
				t.Fatal(err)
			}
			issue := func(p authstate.Policy) authstate.Issued {
				req, _ := json.Marshal(authstate.IssueRequest{Name: "integration reader", TTLSeconds: 600, Policy: p})
				var v authstate.Issued
				if e := json.Unmarshal(tokenRequest(t, srv.URL, "POST", base+"/connections/"+self.ConnectionID+"/tokens", root.Token, bytes.NewReader(req), map[string]string{"Content-Type": "application/json"}, 201), &v); e != nil {
					t.Fatal(e)
				}
				return v
			}
			readPolicy := authstate.Policy{Scopes: []string{"objects:read", "buckets:read"}, Resources: []authstate.Resource{{Bucket: bucket, Prefix: "reports/"}}}
			child := issue(readPolicy)
			childBase := "/api/sessions/" + child.ID
			query := func(key string) string { return url.Values{"bucket": {bucket}, "key": {key}}.Encode() }
			data := tokenRequest(t, srv.URL, "GET", childBase+"/objects/preview?"+query("reports/file.txt"), child.Token, nil, nil, 200)
			if string(data) != "content" {
				t.Fatal("read content changed")
			}
			tokenRequest(t, srv.URL, "GET", childBase+"/objects/preview?"+query("outside/file.txt"), child.Token, nil, nil, 403)
			tokenRequest(t, srv.URL, "GET", childBase+"/admin/users", child.Token, nil, nil, 403)
			tokenRequest(t, srv.URL, "GET", childBase+"/admin/buckets/"+bucket+"/details", child.Token, nil, nil, 403)
			tokenRequest(t, srv.URL, "PUT", childBase+"/objects/upload?"+query("reports/new.txt"), child.Token, strings.NewReader("x"), map[string]string{"Content-Type": "text/plain", "X-Object-Revision": "absent", "X-Upload-Size": "1"}, 403)
			tokenRequest(t, srv.URL, "GET", childBase+"/objects?"+url.Values{"bucket": {bucket}, "prefix": {"reports/"}}.Encode(), child.Token, nil, nil, 200)
			tokenRequest(t, srv.URL, "GET", childBase+"/objects?"+url.Values{"bucket": {bucket}, "prefix": {"outside/"}}.Encode(), child.Token, nil, nil, 403)
			list := tokenRequest(t, srv.URL, "GET", childBase+"/buckets", child.Token, nil, nil, 200)
			if !bytes.Contains(list, []byte(bucket)) {
				t.Fatal("allowed bucket hidden")
			}
			// A missing delete scope rejects a move before copying anything.
			writer := issue(authstate.Policy{Scopes: []string{"objects:read", "objects:write"}, Resources: readPolicy.Resources})
			move, _ := json.Marshal(map[string]any{"sourceBucket": bucket, "sourceKey": "reports/file.txt", "destinationBucket": bucket, "destinationKey": "reports/moved.txt", "move": true})
			tokenRequest(t, srv.URL, "POST", "/api/sessions/"+writer.ID+"/objects/transfer", writer.Token, bytes.NewReader(move), map[string]string{"Content-Type": "application/json"}, 403)
			if _, e := client.StatObject(context.Background(), bucket, "reports/moved.txt", minio.StatObjectOptions{}); e == nil {
				t.Fatal("denied move created destination")
			}
			// Full object scopes within one prefix support actual writes, copy/move,
			// delete and read-only archive preparation, not just rejected requests.
			editor := issue(authstate.Policy{Scopes: []string{"objects:read", "objects:write", "objects:delete", "buckets:read"}, Resources: readPolicy.Resources})
			editorBase := "/api/sessions/" + editor.ID
			tokenRequest(t, srv.URL, "PUT", editorBase+"/objects/upload?"+query("reports/new.txt"), editor.Token, strings.NewReader("x"), map[string]string{"Content-Type": "text/plain", "X-Object-Revision": "absent", "X-Upload-Size": "1"}, 200)
			var deletion struct {
				Revision string `json:"revision"`
			}
			if err = json.Unmarshal(tokenRequest(t, srv.URL, "GET", editorBase+"/admin/object-delete?"+query("reports/new.txt"), editor.Token, nil, nil, 200), &deletion); err != nil {
				t.Fatal(err)
			}
			transfer, _ := json.Marshal(map[string]any{"sourceBucket": bucket, "sourceKey": "reports/new.txt", "sourceRevision": deletion.Revision, "destinationBucket": bucket, "destinationKey": "reports/moved.txt", "destinationRevision": "absent", "move": true})
			var moved struct {
				Moved bool `json:"moved"`
			}
			if err = json.Unmarshal(tokenRequest(t, srv.URL, "POST", editorBase+"/objects/transfer", editor.Token, bytes.NewReader(transfer), map[string]string{"Content-Type": "application/json"}, 200), &moved); err != nil || !moved.Moved {
				t.Fatal("scoped move failed", err)
			}
			if err = json.Unmarshal(tokenRequest(t, srv.URL, "GET", editorBase+"/admin/object-delete?"+query("reports/moved.txt"), editor.Token, nil, nil, 200), &deletion); err != nil {
				t.Fatal(err)
			}
			remove, _ := json.Marshal(map[string]string{"revision": deletion.Revision, "confirmation": "reports/moved.txt"})
			tokenRequest(t, srv.URL, "DELETE", editorBase+"/admin/object-delete?"+query("reports/moved.txt"), editor.Token, bytes.NewReader(remove), map[string]string{"Content-Type": "application/json"}, 200)
			archive, _ := json.Marshal(map[string]any{"bucket": bucket, "keys": []string{"reports/file.txt"}, "name": "reports.zip"})
			tokenRequest(t, srv.URL, "POST", childBase+"/objects/archive", child.Token, bytes.NewReader(archive), map[string]string{"Content-Type": "application/json"}, 200)
			// Absolute expiry, cookie/CSRF and Bearer credentials survive a clean restart.
			beforeExpiry := self.ExpiresAt
			stop()
			start()
			cookie.Origin = srv.URL
			cookie.call("GET", "admin/buckets", nil, 200)
			cookie.call("POST", "admin/buckets", map[string]string{"name": "Invalid_Name"}, 400)
			tokenRequest(t, srv.URL, "GET", childBase+"/objects/preview?"+query("reports/file.txt"), child.Token, nil, nil, 200)
			if err = json.Unmarshal(tokenRequest(t, srv.URL, "GET", base+"/credential", root.Token, nil, nil, 200), &self); err != nil || !self.ExpiresAt.Equal(beforeExpiry) {
				t.Fatal("restart extended expiry", err)
			}
			rotate := []byte(`{"revision":1,"graceSeconds":0}`)
			var next authstate.Issued
			if err = json.Unmarshal(tokenRequest(t, srv.URL, "POST", base+"/tokens/"+child.ID+"/rotate", root.Token, bytes.NewReader(rotate), map[string]string{"Content-Type": "application/json"}, 200), &next); err != nil {
				t.Fatal(err)
			}
			tokenRequest(t, srv.URL, "GET", childBase+"/credential", child.Token, nil, nil, 401)
			stop()
			start()
			tokenRequest(t, srv.URL, "GET", childBase+"/objects/preview?"+query("reports/file.txt"), next.Token, nil, nil, 200)
			tokenRequest(t, srv.URL, "DELETE", base+"/tokens/"+child.ID, root.Token, strings.NewReader(`{"revision":2}`), map[string]string{"Content-Type": "application/json"}, 200)
			stop()
			start()
			tokenRequest(t, srv.URL, "GET", childBase+"/credential", next.Token, nil, nil, 401)
			// Owner is upstream endpoint + identity; a different MinIO account cannot
			// enumerate or modify the saved root connection.
			user := "owner-" + strings.ToLower(strings.ReplaceAll(authstate.Digest(root.ID)[:12], "_", "a"))
			cookie.Origin = srv.URL
			cookie.call("POST", "admin/users", map[string]any{"name": user, "secretKey": "Owner-fixture-password-123", "acknowledgeNonAtomicCreate": true}, 200)
			defer func() {
				cookie.Origin = srv.URL
				cookie.call("DELETE", "admin/users/"+user, map[string]string{"confirmation": user}, 200)
			}()
			cookie.call("POST", "admin/bindings", map[string]any{"user": user, "policy": "readonly"}, 200)
			defer func() {
				cookie.Origin = srv.URL
				cookie.call("POST", "admin/bindings", map[string]any{"user": user, "policy": "readonly", "remove": true}, 200)
			}()
			foreign := issueTestToken(t, srv.URL, user, "Owner-fixture-password-123", nil)
			tokenRequest(t, srv.URL, "GET", "/api/sessions/"+foreign.ID+"/connections/"+self.ConnectionID, foreign.Token, nil, nil, 404)
			// Updating the stored native secret must preserve the existing API
			// token and refresh an already cached SDK client on its next request.
			foreignBase := "/api/sessions/" + foreign.ID
			tokenRequest(t, srv.URL, "GET", foreignBase+"/objects/preview?"+query("reports/file.txt"), foreign.Token, nil, nil, 200)
			admin, err := madmin.New("127.0.0.1:19000", "minio-web-test", "minio-web-test-secret", false)
			if err != nil {
				t.Fatal(err)
			}
			if err = admin.SetUser(context.Background(), user, "Owner-replaced-fixture-password-456", madmin.AccountEnabled); err != nil {
				t.Fatal(err)
			}
			replacement, _ := json.Marshal(map[string]any{"revision": 1, "accessKey": user, "secretKey": "Owner-replaced-fixture-password-456"})
			tokenRequest(t, srv.URL, "PUT", foreignBase+"/connections/"+foreign.ConnectionID+"/credentials", foreign.Token, bytes.NewReader(replacement), map[string]string{"Content-Type": "application/json"}, 200)
			tokenRequest(t, srv.URL, "GET", foreignBase+"/objects/preview?"+query("reports/file.txt"), foreign.Token, nil, nil, 200)
			stop()
			start()
			tokenRequest(t, srv.URL, "GET", foreignBase+"/objects/preview?"+query("reports/file.txt"), foreign.Token, nil, nil, 200)
			// Native signed links can outlive revocation but never the issuing access.
			linkRaw := tokenRequest(t, srv.URL, "GET", base+"/objects/url?"+query("reports/file.txt")+"&expiresIn=86400&mode=download", root.Token, nil, nil, 200)
			var link struct {
				URL string `json:"url"`
			}
			_ = json.Unmarshal(linkRaw, &link)
			if link.URL == "" {
				t.Fatal("missing signed URL")
			}
			if link.URL != "" {
				u, e := url.Parse(link.URL)
				if e != nil {
					t.Fatal(e)
				}
				if u.Query().Get("X-Amz-Expires") == "86400" {
					t.Fatal("signed link outlives access")
				}
			}
			// Reusing a deleted MinIO key name with a different secret must not
			// let the previous API credential adopt the new principal's connection.
			reused := "reused-" + authstate.Digest(root.ID)[:12]
			cookie.Origin = srv.URL
			cookie.call("POST", "admin/users", map[string]any{"name": reused, "secretKey": "Reused-old-fixture-password-123", "acknowledgeNonAtomicCreate": true}, 200)
			cookie.call("POST", "admin/bindings", map[string]any{"user": reused, "policy": "readonly"}, 200)
			oldPrincipal := issueTestToken(t, srv.URL, reused, "Reused-old-fixture-password-123", nil)
			cookie.call("POST", "admin/bindings", map[string]any{"user": reused, "policy": "readonly", "remove": true}, 200)
			cookie.call("DELETE", "admin/users/"+reused, map[string]string{"confirmation": reused}, 200)
			cookie.call("POST", "admin/users", map[string]any{"name": reused, "secretKey": "Reused-new-fixture-password-123", "acknowledgeNonAtomicCreate": true}, 200)
			cookie.call("POST", "admin/bindings", map[string]any{"user": reused, "policy": "readonly"}, 200)
			newPrincipal := issueTestToken(t, srv.URL, reused, "Reused-new-fixture-password-123", nil)
			oldBase := "/api/sessions/" + oldPrincipal.ID
			tokenRequest(t, srv.URL, "GET", oldBase+"/connections", oldPrincipal.Token, nil, nil, 403)
			limited, _ := json.Marshal(authstate.IssueRequest{TTLSeconds: 60, Policy: readPolicy})
			tokenRequest(t, srv.URL, "POST", oldBase+"/connections/"+newPrincipal.ConnectionID+"/tokens", oldPrincipal.Token, bytes.NewReader(limited), map[string]string{"Content-Type": "application/json"}, 403)
			tokenRequest(t, srv.URL, "POST", oldBase+"/tokens/"+newPrincipal.ID+"/rotate", oldPrincipal.Token, strings.NewReader(`{"revision":1}`), map[string]string{"Content-Type": "application/json"}, 403)
			tokenRequest(t, srv.URL, "GET", "/api/sessions/"+newPrincipal.ID+"/objects/preview?"+query("reports/file.txt"), newPrincipal.Token, nil, nil, 200)
			cookie.call("POST", "admin/bindings", map[string]any{"user": reused, "policy": "readonly", "remove": true}, 200)
			cookie.call("DELETE", "admin/users/"+reused, map[string]string{"confirmation": reused}, 200)
			// Validation failure changes nothing. A validated replacement keeps
			// all API credentials usable with unchanged revisions and expiry.
			invalid, _ := json.Marshal(map[string]any{"revision": 1, "accessKey": "minio-web-test", "secretKey": "wrong-credential-secret"})
			tokenRequest(t, srv.URL, "PUT", base+"/connections/"+self.ConnectionID+"/credentials", root.Token, bytes.NewReader(invalid), map[string]string{"Content-Type": "application/json"}, 403)
			tokenRequest(t, srv.URL, "GET", base+"/credential", root.Token, nil, nil, 200)
			valid, _ := json.Marshal(map[string]any{"revision": 1, "accessKey": "minio-web-test", "secretKey": "minio-web-test-secret"})
			tokenRequest(t, srv.URL, "PUT", base+"/connections/"+self.ConnectionID+"/credentials", root.Token, bytes.NewReader(valid), map[string]string{"Content-Type": "application/json"}, 200)
			var stillActive authstate.AccessView
			if err = json.Unmarshal(tokenRequest(t, srv.URL, "GET", base+"/credential", root.Token, nil, nil, 200), &stillActive); err != nil || stillActive.Revision != self.Revision || !stillActive.ExpiresAt.Equal(self.ExpiresAt) {
				t.Fatal("upstream replacement changed API credential", err)
			}
			cookie.Origin = srv.URL
			cookie.call("DELETE", "connections/"+self.ConnectionID, map[string]int{"revision": 2}, 200)
			stop()
			start()
			tokenRequest(t, srv.URL, "GET", "/api/sessions/"+writer.ID+"/credential", writer.Token, nil, nil, 401)
			if self.ExpiresAt.After(time.Now().Add(time.Hour)) {
				t.Fatal("invalid fixture lifetime")
			}
		})
	}
}

func checkRenewableAccessHTTP(t *testing.T, origin string) {
	t.Helper()
	ttl := int64(120)
	service := issueTestToken(t, origin, "minio-web-test", "minio-web-test-secret", &ttl)
	base := "/api/sessions/" + service.ID
	readOnly := authstate.Policy{Scopes: []string{"objects:read"}}
	narrow, _ := json.Marshal(authstate.EditRequest{Revision: 1, Policy: &readOnly})
	tokenRequest(t, origin, "PATCH", base+"/tokens/"+service.ID, service.Token, bytes.NewReader(narrow), map[string]string{"Content-Type": "application/json"}, 200)
	var renewed authstate.AccessView
	if err := json.Unmarshal(tokenRequest(t, origin, "POST", base+"/tokens/"+service.ID+"/renew", service.Token, strings.NewReader(`{"revision":2,"ttlSeconds":900}`), map[string]string{"Content-Type": "application/json"}, 200), &renewed); err != nil || !renewed.ExpiresAt.After(service.ExpiresAt) || renewed.Policy.Allows("access:manage") || renewed.Revision != 3 {
		t.Fatal("read-only service self-renew failed or gained rights", renewed, err)
	}
	tokenRequest(t, origin, "GET", base+"/connections", service.Token, nil, nil, 403)
	tokenRequest(t, origin, "POST", base+"/tokens/"+service.ID+"/renew", service.Token, strings.NewReader(`{"revision":2,"ttlSeconds":900}`), map[string]string{"Content-Type": "application/json"}, 409)

	browser := loginTest(t, origin, "minio-web-test", "minio-web-test-secret")
	var cookie authstate.AccessView
	if err := json.Unmarshal(browser.call("GET", "credential", nil, 200), &cookie); err != nil {
		t.Fatal(err)
	}
	var independent authstate.Issued
	if err := json.Unmarshal(browser.call("POST", "connections/"+cookie.ConnectionID+"/tokens", authstate.IssueRequest{Name: "service independent of browser", TTLSeconds: 600, Policy: readOnly}, 201), &independent); err != nil {
		t.Fatal(err)
	}
	if status, data := browser.request("DELETE", browser.Base, nil); status != 200 {
		t.Fatal("browser logout failed", status, string(data))
	}
	independentBase := "/api/sessions/" + independent.ID
	var view authstate.AccessView
	if err := json.Unmarshal(tokenRequest(t, origin, "GET", independentBase+"/credential", independent.Token, nil, nil, 200), &view); err != nil || view.ParentID != "" || !view.Renewable {
		t.Fatal("browser logout revoked independent service", view, err)
	}
	tokenRequest(t, origin, "POST", independentBase+"/tokens/"+independent.ID+"/renew", independent.Token, strings.NewReader(`{"revision":1,"ttlSeconds":900}`), map[string]string{"Content-Type": "application/json"}, 200)
	tokenRequest(t, origin, "DELETE", independentBase, independent.Token, nil, nil, 200)
	tokenRequest(t, origin, "DELETE", base, service.Token, nil, nil, 200)
}

func checkDelegatedAccessHTTP(t *testing.T, origin, bucket string) {
	t.Helper()
	root := issueTestToken(t, origin, "minio-web-test", "minio-web-test-secret", nil)
	target := issueTestToken(t, origin, "minio-web-test", "minio-web-test-secret", nil)
	rootBase := "/api/sessions/" + root.ID
	issue := func(base, token, connection string, policy authstate.Policy) authstate.Issued {
		t.Helper()
		raw, _ := json.Marshal(authstate.IssueRequest{Name: "delegation fixture", TTLSeconds: 600, Policy: policy})
		var v authstate.Issued
		if err := json.Unmarshal(tokenRequest(t, origin, "POST", base+"/connections/"+connection+"/tokens", token, bytes.NewReader(raw), map[string]string{"Content-Type": "application/json"}, 201), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	parent := issue(rootBase, root.Token, root.ConnectionID, authstate.FullPolicy())
	child := issue("/api/sessions/"+parent.ID, parent.Token, target.ConnectionID, authstate.Policy{Scopes: []string{"objects:read"}, Resources: []authstate.Resource{{Bucket: bucket, Prefix: "reports/"}}})
	childBase := "/api/sessions/" + child.ID
	preview := childBase + "/objects/preview?" + url.Values{"bucket": {bucket}, "key": {"reports/file.txt"}}.Encode()
	tokenRequest(t, origin, "GET", preview, child.Token, nil, nil, 200)
	policy := authstate.Policy{Scopes: []string{"access:manage", "objects:read"}, Resources: []authstate.Resource{{Bucket: bucket, Prefix: "reports/narrow/"}}}
	raw, _ := json.Marshal(authstate.EditRequest{Revision: 1, Policy: &policy})
	tokenRequest(t, origin, "PATCH", rootBase+"/tokens/"+parent.ID, root.Token, bytes.NewReader(raw), map[string]string{"Content-Type": "application/json"}, 200)
	// The child already has a cached runtime session, but the next native
	// request must use the narrowed durable policy from another connection.
	tokenRequest(t, origin, "GET", preview, child.Token, nil, nil, 403)
	var narrowed authstate.AccessView
	if err := json.Unmarshal(tokenRequest(t, origin, "GET", childBase+"/credential", child.Token, nil, nil, 200), &narrowed); err != nil || !policy.Contains(narrowed.Policy) || narrowed.ParentID != parent.ID {
		t.Fatal("HTTP child kept pre-cascade policy", narrowed, err)
	}
	tokenRequest(t, origin, "DELETE", rootBase+"/tokens/"+parent.ID, root.Token, strings.NewReader(`{"revision":2}`), map[string]string{"Content-Type": "application/json"}, 200)
	tokenRequest(t, origin, "GET", childBase+"/credential", child.Token, nil, nil, 401)
	tokenRequest(t, origin, "DELETE", rootBase, root.Token, nil, nil, 200)
	tokenRequest(t, origin, "DELETE", "/api/sessions/"+target.ID, target.Token, nil, nil, 200)
}

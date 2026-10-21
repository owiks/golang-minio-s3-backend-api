package integration

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	apihttp "minio-web/api/src/http/api"
	keyssvc "minio-web/api/src/services/keys"
	runtimesvc "minio-web/api/src/services/runtime"
	"minio-web/api/src/services/securetoken"
	serversvc "minio-web/api/src/services/server"
)

func TestAccessKeyValidationAndRedaction(t *testing.T) {
	for _, input := range []keyssvc.KeyInput{{Name: "bad name", Policy: json.RawMessage("null")}, {Name: "a!", Policy: json.RawMessage("null")}, {Name: "1backup", Policy: json.RawMessage("null")}, {Policy: json.RawMessage(`{}`)}, {Policy: json.RawMessage(`{"Version":"2012-10-17","Statement":[]}`)}, {Policy: json.RawMessage(`{"Version":"2012-10-17","Statement":"invalid"}`)}, {Description: strings.Repeat("а", 129), Policy: json.RawMessage("null")}, {Policy: json.RawMessage("null"), Expiration: new(time.Now().Add(-time.Hour))}, {Policy: json.RawMessage("null"), Expiration: new(time.Now().Add(5 * time.Minute))}, {Policy: json.RawMessage("null"), Expiration: new(time.Now().Add(366 * 24 * time.Hour))}} {
		if keyssvc.ValidateKeyInput(input) == nil {
			t.Fatalf("accepted invalid input: %+v", input)
		}
	}
	if err := keyssvc.ValidateKeyInput(keyssvc.KeyInput{Name: "backup-service_1", Policy: json.RawMessage("null")}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []*time.Time{nil, new(time.Time{}), new(time.Unix(0, 0))} {
		if keyssvc.KeyExpiration(value) != nil {
			t.Fatal("no-expiry sentinel leaked")
		}
	}
	raw, err := json.Marshal(serversvc.ServerRows(madmin.InfoMessage{Servers: []madmin.ServerProperties{{Endpoint: "node:9000", MinioEnvVars: map[string]string{"MINIO_ROOT_PASSWORD": "forbidden-env-secret"}}}}))
	if err != nil || strings.Contains(string(raw), "forbidden") || strings.Contains(string(raw), "env") {
		t.Fatal("server environment exposed", string(raw), err)
	}
}

func TestAccessKeysIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("requires docker-compose.test.yml fixture")
	}
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
	app := runtimesvc.New(cfg)
	defer app.Close()
	srv := httptest.NewServer(apihttp.NewHandler(app))
	defer srv.Close()
	root := loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret")
	suffix := strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:10]))
	user, bucket := "key-user-"+suffix, "key-bucket-"+suffix
	admin, _ := madmin.New("127.0.0.1:19000", "minio-web-test", "minio-web-test-secret", false)
	fixture, _ := minio.New("127.0.0.1:19000", &minio.Options{Creds: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", ""), Region: "us-east-1"})
	ctx := context.Background()
	var created []string
	defer func() {
		for _, key := range created {
			_ = admin.DeleteServiceAccount(ctx, key)
		}
		_ = admin.RemoveUser(ctx, user)
		_ = fixture.RemoveObject(ctx, bucket, "allowed.txt", minio.RemoveObjectOptions{})
		_ = fixture.RemoveBucket(ctx, bucket)
	}()
	root.call("POST", "admin/buckets", map[string]any{"name": bucket}, 200)
	root.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": user, "secretKey": "Key-parent-fixture-123"}, 200)
	root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": "readwrite"}, 200)
	parent := loginTest(t, srv.URL, user, "Key-parent-fixture-123")
	getDetail := func(client *apiClient, key string) keyssvc.AccessKeyDetails {
		t.Helper()
		var value keyssvc.AccessKeyDetails
		raw := client.call("GET", "admin/access-keys/"+key, nil, 200)
		if json.Unmarshal(raw, &value) != nil || value.Revision == "" {
			t.Fatal(string(raw))
		}
		return value
	}
	getList := func(client *apiClient, path string) []keyssvc.AccessKeyRow {
		t.Helper()
		var rows []keyssvc.AccessKeyRow
		raw := client.call("GET", "admin/"+path, nil, 200)
		if json.Unmarshal(raw, &rows) != nil || rows == nil {
			t.Fatal(string(raw))
		}
		if strings.Contains(string(raw), "secretKey") || strings.Contains(string(raw), "sessionToken") {
			t.Fatal("list exposed credentials")
		}
		return rows
	}
	// Root is absent from ListUsers; its application keys must still appear in both lists.
	var ownPair struct {
		AccessKey string `json:"accessKey"`
	}
	if json.Unmarshal(root.call("POST", "admin/access-keys", map[string]any{"name": "root-app", "description": "", "policy": nil, "expiration": nil}, 200), &ownPair) != nil || ownPair.AccessKey == "" {
		t.Fatal("own key creation failed")
	}
	created = append(created, ownPair.AccessKey)
	for _, query := range []string{"access-keys?scope=own", "access-keys?scope=all"} {
		found := false
		for _, row := range getList(root, query) {
			if row.AccessKey == ownPair.AccessKey {
				found = true
				if row.Parent != "minio-web-test" || row.Expiration != nil || row.Status != "on" || row.Name != "root-app" {
					t.Fatal("incorrect own key", row)
				}
			}
		}
		if !found {
			t.Fatal("own root key absent", query)
		}
	}
	parent.call("POST", "admin/access-keys", map[string]any{"parent": "minio-web-test", "name": "forbidden-root-app", "description": "", "policy": nil, "expiration": nil}, 403)
	var pair struct {
		AccessKey string `json:"accessKey"`
		SecretKey string `json:"secretKey"`
	}
	expiry := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	policy := json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:ListAllMyBuckets"],"Resource":["arn:aws:s3:::*"]}]}`)
	raw := root.call("POST", "admin/access-keys", map[string]any{"parent": user, "name": "backup-key", "description": "initial", "policy": policy, "expiration": expiry}, 200)
	if json.Unmarshal(raw, &pair) != nil || pair.AccessKey == "" || pair.SecretKey == "" {
		t.Fatal("missing generated pair")
	}
	created = append(created, pair.AccessKey)
	key := pair.AccessKey
	d := getDetail(root, key)
	if d.Parent != user || d.Inherited || d.Policy == nil || d.Expiration == nil || !d.Expiration.Equal(expiry) || d.Name != "backup-key" {
		t.Fatalf("unexpected detail %+v", d)
	}
	if strings.Contains(string(root.call("GET", "admin/access-keys/"+key, nil, 200)), pair.SecretKey) {
		t.Fatal("secret exposed by details")
	}
	for _, query := range []string{"access-keys?scope=all", "access-keys?scope=user&parent=" + user} {
		rows := getList(root, query)
		found := false
		for _, row := range rows {
			if row.AccessKey == key {
				found = true
				if row.Name != "backup-key" || row.Status != "on" || row.Parent != user {
					t.Fatal("metadata lost in list", query, row)
				}
			}
		}
		if !found {
			t.Fatal("missing key", query, rows)
		}
	}
	getList(parent, "access-keys?scope=own")
	parent.call("GET", "admin/server-info", nil, 403)
	parent.call("GET", "admin/access-keys?scope=all", nil, 403)
	keySession := loginTest(t, srv.URL, key, pair.SecretKey)
	keySession.call("DELETE", "admin/access-keys/"+key, map[string]any{"confirmation": key}, 409)
	keySession.call("PUT", "admin/access-keys/"+key, map[string]any{"name": "self-edit", "policy": nil, "revision": d.Revision}, 409)
	keySession.call("PUT", "admin/access-keys/"+key+"/status", map[string]any{"enabled": false, "revision": d.Revision}, 409)
	keySession.call("POST", "admin/access-keys/"+key+"/rotate", map[string]any{"confirmation": key, "revision": d.Revision}, 409)
	keyClient, _ := minio.New("127.0.0.1:19000", &minio.Options{Creds: credentials.NewStaticV4(key, pair.SecretKey, ""), Region: "us-east-1"})
	if _, err := keyClient.PutObject(ctx, bucket, "allowed.txt", strings.NewReader("x"), 1, minio.PutObjectOptions{}); minio.ToErrorResponse(err).Code != "AccessDenied" {
		t.Fatal("inline policy failed to restrict upload", err)
	}
	// The edit command must retain both the custom policy and unchanged deadline.
	previous := d.Revision
	updated := root.call("PUT", "admin/access-keys/"+key, map[string]any{"name": "backup-edited", "description": "edited", "policy": d.Policy, "expiration": expiry, "revision": previous}, 200)
	if strings.TrimSpace(string(updated)) != `{"ok":true}` {
		t.Fatal("edit response contract changed", string(updated))
	}
	d = getDetail(root, key)
	if d.Revision == previous || d.Name != "backup-edited" || d.Description != "edited" || d.Inherited || d.Expiration == nil || !d.Expiration.Equal(expiry) {
		t.Fatal("edit changed restrictions or lost metadata")
	}
	// Every command rejects an old revision without changing state or returning a secret.
	for _, tc := range []struct {
		method, action string
		body           map[string]any
	}{
		{"PUT", "", map[string]any{"name": "stale-edit", "policy": nil, "revision": previous}},
		{"PUT", "/status", map[string]any{"enabled": false, "revision": previous}},
		{"POST", "/rotate", map[string]any{"confirmation": key, "revision": previous}},
	} {
		rejected := root.call(tc.method, "admin/access-keys/"+key+tc.action, tc.body, 409)
		var result struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(rejected, &result) != nil || result.Error.Code != "AccessKeyChanged" || strings.Contains(string(rejected), "secretKey") {
			t.Fatal("incorrect revision failure", string(rejected))
		}
		if getDetail(root, key).Revision != d.Revision {
			t.Fatal("rejected command changed key")
		}
	}
	updated = root.call("PUT", "admin/access-keys/"+key+"/status", map[string]any{"enabled": false, "revision": d.Revision}, 200)
	if strings.TrimSpace(string(updated)) != `{"ok":true}` {
		t.Fatal("status response contract changed", string(updated))
	}
	d = getDetail(root, key)
	root.call("PUT", "admin/access-keys/"+key+"/status", map[string]any{"enabled": true, "revision": d.Revision}, 200)
	d = getDetail(root, key)
	if d.Inherited {
		t.Fatal("status update removed inline policy")
	}
	var customRotation struct {
		SecretKey string `json:"secretKey"`
	}
	if json.Unmarshal(root.call("POST", "admin/access-keys/"+key+"/rotate", map[string]any{"confirmation": key, "revision": d.Revision}, 200), &customRotation) != nil || customRotation.SecretKey == "" {
		t.Fatal("missing rotation secret")
	}
	d = getDetail(root, key)
	if d.Inherited {
		t.Fatal("rotation removed inline policy")
	}
	keyClient, _ = minio.New("127.0.0.1:19000", &minio.Options{Creds: credentials.NewStaticV4(key, customRotation.SecretKey, ""), Region: "us-east-1"})
	if _, err := keyClient.PutObject(ctx, bucket, "allowed.txt", strings.NewReader("x"), 1, minio.PutObjectOptions{}); minio.ToErrorResponse(err).Code != "AccessDenied" {
		t.Fatal("rotation expanded permissions", err)
	}
	root.call("PUT", "admin/access-keys/"+key, map[string]any{"name": "backup-updated", "description": "updated", "policy": nil, "expiration": nil, "revision": d.Revision}, 200)
	root.call("PUT", "admin/access-keys/"+key+"/status", map[string]any{"enabled": false, "revision": d.Revision}, 409)
	d = getDetail(root, key)
	if !d.Inherited || d.Expiration != nil || d.Name != "backup-updated" || d.Description != "updated" {
		t.Fatalf("inherit/reset expiry failed: %+v", d)
	}
	if _, err := keyClient.PutObject(ctx, bucket, "allowed.txt", strings.NewReader("x"), 1, minio.PutObjectOptions{}); err != nil {
		t.Fatal("inherit did not restore parent permissions", err)
	}
	root.call("PUT", "admin/access-keys/"+key+"/status", map[string]any{"enabled": false, "revision": d.Revision}, 200)
	if _, err := keyClient.ListBuckets(ctx); err == nil {
		t.Fatal("disabled key accepted")
	}
	d = getDetail(root, key)
	root.call("PUT", "admin/access-keys/"+key+"/status", map[string]any{"enabled": true, "revision": d.Revision}, 200)
	d = getDetail(root, key)
	root.call("POST", "admin/access-keys/"+key+"/rotate", map[string]any{"confirmation": "wrong", "revision": d.Revision}, 400)
	var rotated struct {
		AccessKey string `json:"accessKey"`
		SecretKey string `json:"secretKey"`
	}
	if json.Unmarshal(root.call("POST", "admin/access-keys/"+key+"/rotate", map[string]any{"confirmation": key, "revision": d.Revision}, 200), &rotated) != nil || rotated.SecretKey == "" || rotated.SecretKey == pair.SecretKey {
		t.Fatal("secret rotation failed")
	}
	if _, err := keyClient.ListBuckets(ctx); err == nil {
		t.Fatal("old secret still accepted")
	}
	loginTest(t, srv.URL, key, rotated.SecretKey)
	root.call("DELETE", "admin/access-keys/"+key, map[string]any{"confirmation": "wrong"}, 400)
	root.call("DELETE", "admin/access-keys/"+key, map[string]any{"confirmation": key}, 200)
	root.call("GET", "admin/access-keys/"+key, nil, 404)
	var snapshot serversvc.ServerSnapshot
	raw = root.call("GET", "admin/server-info", nil, 200)
	if json.Unmarshal(raw, &snapshot) != nil || len(snapshot.Nodes) == 0 || len(snapshot.Drives) == 0 || snapshot.Nodes[0].Version == "" {
		t.Fatal("missing actual nodes/drives", string(raw))
	}
	if snapshot.Nodes[0].Uptime < 0 || snapshot.Nodes[0].Uptime > 86400 {
		t.Fatal("uptime is not seconds", snapshot.Nodes[0].Uptime)
	}
	sts, err := credentials.NewSTSAssumeRole("http://127.0.0.1:19000", credentials.STSAssumeRoleOptions{AccessKey: user, SecretKey: "Key-parent-fixture-123", DurationSeconds: 900})
	if err != nil {
		t.Fatal(err)
	}
	token, err := sts.Get()
	if err != nil {
		t.Fatal(err)
	}
	rows := getList(root, "temporary-access?scope=all")
	found := false
	for _, row := range rows {
		if row.AccessKey == token.AccessKeyID {
			found = true
			if row.Parent != user || row.Expiration == nil {
				t.Fatal("bad STS row", row)
			}
		}
	}
	if !found {
		t.Fatal("STS key absent", rows)
	}
	stsRaw := root.call("GET", "admin/temporary-access/"+token.AccessKeyID, nil, 200)
	if strings.Contains(string(stsRaw), token.SecretAccessKey) || strings.Contains(string(stsRaw), token.SessionToken) {
		t.Fatal("STS detail exposed secrets")
	}
	root.call("DELETE", "admin/temporary-access/"+token.AccessKeyID, map[string]any{"confirmation": token.AccessKeyID}, 405)
}

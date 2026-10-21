package integration

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/minio/minio-go/v7/pkg/credentials"

	apihttp "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/iam"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

func TestActionAccessIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("isolated fixture only")
	}
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
	app := runtimesvc.New(cfg)
	defer app.Close()
	srv := httptest.NewServer(apihttp.NewHandler(app))
	defer srv.Close()
	root := loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret")
	check := func(c *apiClient, queries []iam.Query, want []bool) {
		t.Helper()
		var v struct {
			Allowed []bool `json:"allowed"`
			Known   bool   `json:"known"`
		}
		raw := c.call("POST", "action-access", map[string]any{"queries": queries}, 200)
		if e := json.Unmarshal(raw, &v); e != nil {
			t.Fatal(e)
		}
		if !v.Known || len(v.Allowed) != len(want) {
			t.Fatalf("invalid permission response %s", raw)
		}
		for i, b := range want {
			if v.Allowed[i] != b {
				t.Fatalf("query %+v got %s", queries[i], raw)
			}
		}
	}
	check(root, []iam.Query{{Action: "admin:CreateUser"}, {Action: "s3:CreateBucket", Bucket: "*"}, {Action: "s3:PutObject", Bucket: "test-action", Key: "file.txt"}, {Action: "admin:DeleteUser", Key: "minio-web-test"}}, []bool{true, true, true, false})
	user := "action-user-" + securetoken.New()[:8]
	name := "action-policy-" + securetoken.New()[:8]
	root.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": user, "secretKey": "Action-fixture-secret"}, 200)
	defer root.call("DELETE", "admin/users/"+user, map[string]any{"confirmation": user}, 200)
	doc := json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:ListUsers","admin:EnableUser"],"Resource":["*"]},{"Effect":"Allow","Action":["s3:ListAllMyBuckets"],"Resource":["arn:aws:s3:::*"]},{"Effect":"Allow","Action":["s3:PutObject","s3:GetObject","s3:DeleteObject"],"Resource":["arn:aws:s3:::action-data/uploads/*"]},{"Effect":"Deny","Action":["s3:DeleteObject"],"Resource":["arn:aws:s3:::action-data/uploads/keep.txt"]}]}`)
	root.call("POST", "admin/policies", map[string]any{"acknowledgeNonAtomicCreate": true, "name": name, "document": doc}, 200)
	defer root.call("DELETE", "admin/policies/"+name, map[string]any{"confirmation": name}, 200)
	root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": name}, 200)
	defer root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": name, "remove": true}, 200)
	limited := loginTest(t, srv.URL, user, "Action-fixture-secret")
	queries := []iam.Query{{Action: "admin:CreateUser"}, {Action: "admin:EnableUser"}, {Action: "s3:PutObject", Bucket: "action-data", Key: "uploads/a.txt"}, {Action: "s3:PutObject", Bucket: "action-data", Key: "other/a.txt"}, {Action: "s3:DeleteObject", Bucket: "action-data", Key: "uploads/keep.txt"}}
	check(limited, queries, []bool{false, true, true, false, false})
	var pair struct{ AccessKey, SecretKey string }
	json.Unmarshal(root.call("POST", "admin/access-keys", map[string]any{"name": "action-fixture", "policy": doc, "expiration": nil}, 200), &pair)
	defer root.call("DELETE", "admin/access-keys/"+pair.AccessKey, map[string]any{"confirmation": pair.AccessKey}, 200)
	derived := loginTest(t, srv.URL, pair.AccessKey, pair.SecretKey)
	check(derived, queries, []bool{false, true, true, false, false})

	sts, err := credentials.NewSTSAssumeRole("http://127.0.0.1:19000", credentials.STSAssumeRoleOptions{AccessKey: user, SecretKey: "Action-fixture-secret", DurationSeconds: 900, Policy: `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:ListUsers"],"Resource":["*"]},{"Effect":"Allow","Action":["s3:ListAllMyBuckets"],"Resource":["arn:aws:s3:::*"]},{"Effect":"Allow","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::action-data/uploads/a.txt"]}]}`})
	if err != nil {
		t.Fatal(err)
	}
	token, err := sts.Get()
	if err != nil {
		t.Fatal(err)
	}
	temporary := &apiClient{T: t, Http: root.Http, Origin: srv.URL}
	status, raw := temporary.request("POST", "/api/sessions", runtimesvc.Connection{Endpoint: "http://127.0.0.1:19000", AccessKey: token.AccessKeyID, SecretKey: token.SecretAccessKey, SessionToken: token.SessionToken, Region: "us-east-1"})
	if status != 201 {
		t.Fatalf("STS login %d: %s", status, raw)
	}
	var sessionReply struct{ ID, CSRF string }
	if err = json.Unmarshal(raw, &sessionReply); err != nil {
		t.Fatal(err)
	}
	temporary.Base = "/api/sessions/" + sessionReply.ID
	temporary.CSRF = sessionReply.CSRF
	check(temporary, queries, []bool{false, false, true, false, false})

	var state struct {
		Revision string `json:"revision"`
	}
	json.Unmarshal(root.call("GET", "admin/policies/"+name, nil, 200), &state)
	root.call("PUT", "admin/policies/"+name, map[string]any{"revision": state.Revision, "document": json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:ListUsers"],"Resource":["*"]}]}`)}, 200)
	limited.call("GET", "access", nil, 200)
	check(limited, queries, []bool{false, false, false, false, false})
	limited.call("POST", "action-access", map[string]any{"queries": []iam.Query{{Action: "admin:MadeUp"}}}, 400)
	limited.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": "must-not-exist", "secretKey": "Never-created-fixture"}, 403)
}

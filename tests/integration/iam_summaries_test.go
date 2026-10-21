package integration

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/minio/madmin-go/v3"

	apihttp "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/presentation"
	identitiessvc "github.com/owiks/golang-minio-s3-backend-api/src/services/identities"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

func TestPolicySummary(t *testing.T) {
	value, err := identitiessvc.SummarizePolicy(json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject","s3:GetObject"],"Resource":"arn:aws:s3:::reports/*","Condition":{"StringEquals":{"s3:prefix":"private-condition-not-forwarded"}}},{"Effect":"Deny","NotAction":"s3:ListBucket","NotResource":["arn:aws:s3:::public"]}]}`))
	if err != nil || value.Statements != 2 || value.Allow != 1 || value.Deny != 1 || !value.HasConditions || !reflect.DeepEqual(value.Actions, []string{"s3:GetObject"}) || !reflect.DeepEqual(value.NotActions, []string{"s3:ListBucket"}) || !reflect.DeepEqual(value.NotResources, []string{"arn:aws:s3:::public"}) {
		t.Fatal(value, err)
	}
	raw, _ := json.Marshal(value)
	if strings.Contains(string(raw), "private-condition-not-forwarded") {
		t.Fatal("condition values exposed")
	}
	value, err = identitiessvc.SummarizePolicy(json.RawMessage(`{"Version":"2012-10-17","Statement":{"Effect":"Allow","Action":"admin:*","Resource":["*"]}}`))
	if err != nil || value.Statements != 1 || value.Allow != 1 || len(value.Resources) != 1 {
		t.Fatal(value, err)
	}
	for _, raw := range []string{`null`, `{}`, `{"Version":"2012-10-17","Statement":null}`, `{"Version":"2012-10-17","Statement":[{"Effect":"Other","Action":"*"}]}`, `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":1}]}`, `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Resource":null}]}`} {
		if _, err := identitiessvc.SummarizePolicy(json.RawMessage(raw)); err == nil {
			t.Fatal("accepted unknown definition", raw)
		}
	}
}

func TestIAMSummariesIntegration(t *testing.T) {
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
	admin, _ := madmin.New("127.0.0.1:19000", "minio-web-test", "minio-web-test-secret", false)
	ctx := context.Background()
	suffix := strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:10]))
	user, group, policy, limitedPolicy := "summary-user-"+suffix, "summary-group-"+suffix, "summary-policy-"+suffix, "summary-limited-"+suffix
	defer func() {
		_, _ = admin.DetachPolicy(ctx, madmin.PolicyAssociationReq{User: user, Policies: []string{policy, limitedPolicy}})
		_, _ = admin.DetachPolicy(ctx, madmin.PolicyAssociationReq{Group: group, Policies: []string{policy}})
		_ = admin.UpdateGroupMembers(ctx, madmin.GroupAddRemove{Group: group, Members: []string{user}, IsRemove: true})
		_ = admin.UpdateGroupMembers(ctx, madmin.GroupAddRemove{Group: group, IsRemove: true})
		_ = admin.RemoveUser(ctx, user)
		_ = admin.RemoveCannedPolicy(ctx, policy)
		_ = admin.RemoveCannedPolicy(ctx, limitedPolicy)
	}()
	root.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": user, "secretKey": "Summary-fixture-secret-123"}, 200)
	root.call("POST", "admin/groups", map[string]any{"acknowledgeNonAtomicCreate": true, "name": group, "members": []string{user}}, 200)
	profile := root.call("GET", "admin/users/"+user, nil, 200)
	var fields map[string]json.RawMessage
	if json.Unmarshal(profile, &fields) != nil || len(fields) != 5 || fields["current"] == nil || strings.Contains(string(profile), "Summary-fixture-secret-123") || strings.Contains(string(profile), "secretKey") {
		t.Fatal("profile exposes unexpected fields", string(profile))
	}
	var memberOf []string
	if json.Unmarshal(fields["groups"], &memberOf) != nil || !reflect.DeepEqual(memberOf, []string{group}) {
		t.Fatal("profile membership missing", string(profile))
	}
	doc := json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::reports/*"]},{"Effect":"Deny","Action":["s3:DeleteObject"],"Resource":["arn:aws:s3:::reports/*"]}]}`)
	root.call("POST", "admin/policies", map[string]any{"acknowledgeNonAtomicCreate": true, "name": policy, "document": doc}, 200)
	root.call("POST", "admin/bindings", map[string]any{"group": group, "policy": policy}, 200)
	root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": policy}, 200)
	getGroups := func(c *apiClient) presentation.GroupSummary {
		t.Helper()
		var rows []presentation.GroupSummary
		raw := c.call("GET", "admin/group-summaries", nil, 200)
		if json.Unmarshal(raw, &rows) != nil {
			t.Fatal(string(raw))
		}
		for _, row := range rows {
			if row.Name == group {
				return row
			}
		}
		t.Fatal("missing group")
		return presentation.GroupSummary{}
	}
	getPolicy := func(c *apiClient) presentation.PolicySummary {
		t.Helper()
		var rows []presentation.PolicySummary
		raw := c.call("GET", "admin/policy-summaries", nil, 200)
		if json.Unmarshal(raw, &rows) != nil {
			t.Fatal(string(raw))
		}
		for _, row := range rows {
			if row.Name == policy {
				return row
			}
		}
		t.Fatal("missing policy")
		return presentation.PolicySummary{}
	}
	var bindings madmin.PolicyEntitiesResult
	if json.Unmarshal(root.call("GET", "admin/bindings?policy="+policy, nil, 200), &bindings) != nil || len(bindings.PolicyMappings) != 1 || bindings.PolicyMappings[0].Policy != policy || !reflect.DeepEqual(bindings.PolicyMappings[0].Users, []string{user}) || !reflect.DeepEqual(bindings.PolicyMappings[0].Groups, []string{group}) {
		t.Fatal("policy recipients are not exact", bindings)
	}
	root.call("GET", "admin/bindings?policy="+policy+"&user="+user, nil, 400)
	root.call("GET", "admin/bindings?policy=bad%2Fname", nil, 400)
	g := getGroups(root)
	if g.Error != "" || g.Details == nil || g.Details.Status != "enabled" || !reflect.DeepEqual(g.Details.Members, []string{user}) || !reflect.DeepEqual(g.Details.Policies, []string{policy}) {
		t.Fatal(g)
	}
	p := getPolicy(root)
	if p.Definition == nil || p.Definition.Allow != 1 || p.Definition.Deny != 1 || p.Assignments == nil || !reflect.DeepEqual(p.Assignments.Users, []string{user}) || !reflect.DeepEqual(p.Assignments.Groups, []string{group}) || p.CreatedAt == nil || p.UpdatedAt == nil || p.DatesError != "" {
		t.Fatalf("unexpected policy summary: %+v", p)
	}
	root.call("PUT", "admin/groups/"+group+"/status", map[string]any{"enabled": false}, 200)
	if getGroups(root).Details.Status != "disabled" {
		t.Fatal("stale group status")
	}
	root.call("PUT", "admin/groups/"+group+"/status", map[string]any{"enabled": true}, 200)
	root.call("POST", "admin/bindings", map[string]any{"group": group, "policy": policy, "remove": true}, 200)
	if len(getGroups(root).Details.Policies) != 0 || len(getPolicy(root).Assignments.Groups) != 0 {
		t.Fatal("stale assignments")
	}
	// The base lists remain usable when optional group detail/date reads are denied.
	limited := json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:ListAllMyBuckets"],"Resource":["arn:aws:s3:::*"]},{"Effect":"Allow","Action":["admin:GetUser","admin:ListGroups","admin:ListUserPolicies"],"Resource":["*"]}]}`)
	root.call("POST", "admin/policies", map[string]any{"acknowledgeNonAtomicCreate": true, "name": limitedPolicy, "document": limited}, 200)
	root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": policy, "remove": true}, 200)
	root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": limitedPolicy}, 200)
	limitedClient := loginTest(t, srv.URL, user, "Summary-fixture-secret-123")
	ownProfile := limitedClient.call("GET", "admin/users/"+user, nil, 200)
	var own struct {
		Current bool `json:"current"`
	}
	if json.Unmarshal(ownProfile, &own) != nil || !own.Current {
		t.Fatal("current account was not identified", string(ownProfile))
	}
	g = getGroups(limitedClient)
	if g.Details != nil || g.Error == "" {
		t.Fatal("denied group read became an empty group", g)
	}
	p = getPolicy(limitedClient)
	if p.Definition == nil || p.Assignments == nil || p.AssignmentsError != "" || p.DatesError == "" || p.CreatedAt != nil || p.UpdatedAt != nil {
		t.Fatalf("partial policy read failed: %+v", p)
	}
	// The policy-centric read uses the caller's MinIO rights, just like identity-centric reads.
	limitedClient.call("GET", "admin/bindings?policy="+policy, nil, 200)
	var current struct {
		Revision string `json:"revision"`
	}
	json.Unmarshal(root.call("GET", "admin/policies/"+limitedPolicy, nil, 200), &current)
	root.call("PUT", "admin/policies/"+limitedPolicy, map[string]any{"revision": current.Revision, "document": json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:ListAllMyBuckets"],"Resource":["arn:aws:s3:::*"]}]}`)}, 200)
	limitedClient.call("GET", "admin/bindings?policy="+policy, nil, 403)
}

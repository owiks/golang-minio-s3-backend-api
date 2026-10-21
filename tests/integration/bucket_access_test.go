package integration

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	apihttp "minio-web/api/src/http/api"
	"minio-web/api/src/http/presentation"
	bucketssvc "minio-web/api/src/services/buckets"
	runtimesvc "minio-web/api/src/services/runtime"
	"minio-web/api/src/services/securetoken"
)

func TestBucketGrantScopeAndRedaction(t *testing.T) {
	for _, tc := range []struct {
		Resource string `json:"-"`
		Want     bool   `json:"-"`
	}{{"*", true}, {"arn:aws:s3:::reports", true}, {"arn:aws:s3:::reports/daily/*", true}, {"arn:aws:s3:::rep*/*", true}, {"arn:aws:s3:::reports-other/*", false}, {"arn:aws:s3:::${bucket}/*", true}} {
		if got := bucketssvc.ResourceTouchesBucket(tc.Resource, "reports"); got != tc.Want {
			t.Fatalf("resource %s: %v", tc.Resource, got)
		}
	}
	raw := json.RawMessage(`{"Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"arn:aws:s3:::reports/daily/*","Condition":{"StringEquals":{"private-key":"not-for-browser"}}},{"Effect":"Deny","NotAction":"s3:GetObject","NotResource":"arn:aws:s3:::other/*"},{"Effect":"Allow","Action":"admin:*","Resource":"*"},{"Effect":"Allow","Action":"s3:GetObject","Resource":"arn:aws:s3:::reports-other/*"}]}`)
	grants, err := bucketssvc.BucketGrants(raw, "reports")
	if err != nil || len(grants) != 2 || !grants[0].Conditional || !grants[1].Uncertain {
		t.Fatal(grants, err)
	}
	encoded, _ := json.Marshal(grants)
	if strings.Contains(string(encoded), "not-for-browser") || strings.Contains(string(encoded), "private-key") {
		t.Fatal("condition values leaked")
	}
	if _, err := bucketssvc.BucketGrants(json.RawMessage(`{"Statement":[{"Effect":"Allow","Action":null}]}`), "reports"); err == nil {
		t.Fatal("malformed policy accepted")
	}
}
func TestBucketAccessIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("requires isolated docker-compose.test.yml")
	}
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
	app := runtimesvc.New(cfg)
	defer app.Close()
	srv := httptest.NewServer(apihttp.NewHandler(app))
	defer srv.Close()
	root := loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret")
	admin, _ := madmin.New("127.0.0.1:19000", "minio-web-test", "minio-web-test-secret", false)
	s3, _ := minio.New("127.0.0.1:19000", &minio.Options{Creds: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", ""), Region: "us-east-1"})
	ctx := context.Background()
	suffix := strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:8]))
	bucket, user, group, policy, limited := "access-bucket-"+suffix, "access-user-"+suffix, "access-group-"+suffix, "access-policy-"+suffix, "access-limited-"+suffix
	key := ""
	defer func() {
		if key != "" {
			_ = admin.DeleteServiceAccount(ctx, key)
		}
		_, _ = admin.DetachPolicy(ctx, madmin.PolicyAssociationReq{User: user, Policies: []string{policy, limited}})
		_, _ = admin.DetachPolicy(ctx, madmin.PolicyAssociationReq{Group: group, Policies: []string{policy}})
		_ = admin.UpdateGroupMembers(ctx, madmin.GroupAddRemove{Group: group, Members: []string{user}, IsRemove: true})
		_ = admin.UpdateGroupMembers(ctx, madmin.GroupAddRemove{Group: group, IsRemove: true})
		_ = admin.RemoveUser(ctx, user)
		_ = admin.RemoveCannedPolicy(ctx, policy)
		_ = admin.RemoveCannedPolicy(ctx, limited)
		_ = s3.SetBucketPolicy(ctx, bucket, "")
		_ = s3.RemoveBucket(ctx, bucket)
	}()
	root.call("POST", "admin/buckets", map[string]any{"name": bucket}, 200)
	root.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": user, "secretKey": "Bucket-access-test-secret"}, 200)
	root.call("POST", "admin/groups", map[string]any{"acknowledgeNonAtomicCreate": true, "name": group, "members": []string{user}}, 200)
	doc := map[string]any{"Version": "2012-10-17", "Statement": []any{map[string]any{"Effect": "Allow", "Action": []string{"s3:GetObject"}, "Resource": []string{"arn:aws:s3:::" + bucket + "/daily/*"}}, map[string]any{"Effect": "Deny", "Action": []string{"s3:DeleteObject"}, "Resource": []string{"arn:aws:s3:::" + bucket + "/*"}}}}
	root.call("POST", "admin/policies", map[string]any{"acknowledgeNonAtomicCreate": true, "name": policy, "document": doc}, 200)
	root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": policy}, 200)
	root.call("POST", "admin/bindings", map[string]any{"group": group, "policy": policy}, 200)
	var pair struct {
		AccessKey string `json:"accessKey"`
		SecretKey string `json:"secretKey"`
	}
	json.Unmarshal(root.call("POST", "admin/access-keys", map[string]any{"parent": user, "name": "test-access", "policy": doc}, 200), &pair)
	key = pair.AccessKey
	read := func(client *apiClient) presentation.BucketAccessReport {
		t.Helper()
		var report presentation.BucketAccessReport
		raw := client.call("GET", "admin/buckets/"+bucket+"/access", nil, 200)
		if json.Unmarshal(raw, &report) != nil {
			t.Fatal(string(raw))
		}
		if strings.Contains(string(raw), pair.SecretKey) || strings.Contains(string(raw), "Bucket-access-test-secret") {
			t.Fatal("credentials leaked")
		}
		return report
	}
	find := func(report presentation.BucketAccessReport, kind, name string) presentation.BucketAccessRow {
		t.Helper()
		for _, row := range report.Rows {
			if row.Kind == kind && row.Name == name {
				return row
			}
		}
		t.Fatalf("missing %s %s: %+v", kind, name, report)
		return presentation.BucketAccessRow{}
	}
	report := read(root)
	if !report.Complete {
		t.Fatalf("unexpected partial report: %+v", report.Warnings)
	}
	if len(find(report, "user", user).Grants) != 4 || len(find(report, "group", group).Grants) != 2 {
		t.Fatal("direct/group grants missing")
	}
	row := find(report, "access-key", key)
	if row.RestrictionState != "restricted" || len(row.Restrictions) != 2 || len(row.Grants) == 0 || !strings.HasPrefix(row.Grants[0].Source, "Owner "+user+":") {
		t.Fatalf("key restrictions missing %+v", row)
	}
	root.call("PUT", "admin/groups/"+group+"/status", map[string]any{"enabled": false}, 200)
	report = read(root)
	if find(report, "group", group).Status != "disabled" {
		t.Fatal("stale group status")
	}
	hasInactive := false
	for _, grant := range find(report, "user", user).Grants {
		hasInactive = hasInactive || strings.Contains(grant.Source, "disabled")
	}
	if !hasInactive {
		t.Fatal("inactive source not labeled")
	}
	// Bucket-policy recipients are independent of IAM assignments.
	bucketPolicy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::` + bucket + `/public/*"}]}`
	if err := s3.SetBucketPolicy(ctx, bucket, bucketPolicy); err != nil {
		t.Fatal(err)
	}
	report = read(root)
	if len(find(report, "bucket-policy", bucket).Grants[0].Principals) != 1 {
		t.Fatal("bucket principal missing")
	}
	root.call("PUT", "admin/groups/"+group+"/status", map[string]any{"enabled": true}, 200)
	// A policy reader without identity/key permissions gets an explicitly partial
	// result. A storage-only user cannot enumerate policies through this endpoint.
	restricted := json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:ListUserPolicies"],"Resource":["*"]}]}`)
	root.call("POST", "admin/policies", map[string]any{"acknowledgeNonAtomicCreate": true, "name": limited, "document": restricted}, 200)
	root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": limited}, 200)
	reader := loginTest(t, srv.URL, user, "Bucket-access-test-secret")
	if value := read(reader); value.Complete || len(value.Warnings) == 0 {
		t.Fatal("partial access presented as complete")
	}
	root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": limited, "remove": true}, 200)
	reader.call("GET", "admin/buckets/"+bucket+"/access", nil, 403)
}

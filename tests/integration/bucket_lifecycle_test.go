package integration

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"

	apihttp "minio-web/api/src/http/api"
	bucketscontroller "minio-web/api/src/http/api/routes/buckets/controllers"
	"minio-web/api/src/http/presentation"
	"minio-web/api/src/services/apperror"
	bucketssvc "minio-web/api/src/services/buckets"
	"minio-web/api/src/services/messages"
	runtimesvc "minio-web/api/src/services/runtime"
	"minio-web/api/src/services/securetoken"
	"minio-web/api/tests/testsession"
)

func TestLifecycleRoundTripGuard(t *testing.T) {
	valid := `<LifecycleConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Rule><ID>logs</ID><Status>Enabled</Status><Filter><And><Prefix> logs/ </Prefix><Tag><Key>environment</Key><Value> dev </Value></Tag></And></Filter><Expiration><Days>30</Days></Expiration><NoncurrentVersionExpiration><NoncurrentDays>90</NoncurrentDays><NewerNoncurrentVersions>3</NewerNoncurrentVersions></NoncurrentVersionExpiration></Rule></LifecycleConfiguration>`
	var cfg lifecycle.Configuration
	if err := xml.Unmarshal([]byte(valid), &cfg); err != nil {
		t.Fatal(err)
	}
	if !bucketssvc.LifecycleRoundTripSafe([]byte(valid), &cfg) {
		t.Fatal("valid rule flagged unsafe")
	}
	for _, raw := range []string{strings.Replace(valid, "</Rule>", "<FutureAction><Days>10</Days></FutureAction></Rule>", 1), strings.Replace(valid, "<Rule>", `<Rule custom="unknown">`, 1), strings.Replace(valid, "</Rule>", "<Transition><Days>5</Days><StorageClass>A</StorageClass></Transition><Transition><Days>10</Days><StorageClass>B</StorageClass></Transition></Rule>", 1), strings.Replace(valid, "</Rule>", "<FilterUnknown/></Rule>", 1)} {
		if err := xml.Unmarshal([]byte(raw), &cfg); err != nil {
			t.Fatal(err)
		}
		if bucketssvc.LifecycleRoundTripSafe([]byte(raw), &cfg) {
			t.Fatal("lossy SDK round trip allowed", raw)
		}
	}
}
func TestLifecycleEditPreservesOtherActions(t *testing.T) {
	old := lifecycle.Rule{ID: "before", Status: "Enabled", RuleFilter: lifecycle.Filter{And: lifecycle.And{Prefix: "before/", Tags: []lifecycle.Tag{{Key: "env", Value: "dev"}}, ObjectSizeGreaterThan: 1024}}, Transition: lifecycle.Transition{Days: 12, StorageClass: "COLD"}, NoncurrentVersionTransition: lifecycle.NoncurrentVersionTransition{NoncurrentDays: 20, StorageClass: "COLD"}, Compression: &lifecycle.Compression{Days: 7, HighCompression: true}, Expiration: lifecycle.Expiration{Days: 30}}
	input := bucketssvc.LifecycleRuleInput(old)
	input.ID = "after"
	input.Prefix = "after/"
	input.ExpirationDays = 60
	updated, err := bucketssvc.ApplyLifecycleInput(old, input)
	if err != nil || updated.ID != "after" || updated.RuleFilter.And.Prefix != "after/" || !reflect.DeepEqual(updated.RuleFilter.And.Tags, old.RuleFilter.And.Tags) || updated.RuleFilter.And.ObjectSizeGreaterThan != 1024 || !reflect.DeepEqual(updated.Transition, old.Transition) || !reflect.DeepEqual(updated.NoncurrentVersionTransition, old.NoncurrentVersionTransition) || !reflect.DeepEqual(updated.Compression, old.Compression) {
		t.Fatal(updated, err)
	}
	// Prefix removal keeps a single Tag valid without a one-condition And.
	old.RuleFilter.And.ObjectSizeGreaterThan = 0
	input.Prefix = ""
	updated, err = bucketssvc.ApplyLifecycleInput(old, input)
	if err != nil || updated.RuleFilter.Tag.Key != "env" || !updated.RuleFilter.And.IsEmpty() {
		t.Fatal(updated, err)
	}
}
func TestLifecycleValidation(t *testing.T) {
	base := bucketssvc.LifecycleInput{ID: "logs", Enabled: true, Prefix: "logs/", ExpirationDays: 30}
	invalid := []bucketssvc.LifecycleInput{{}, {ID: "logs"}, {ID: "logs", ExpirationDays: -1}, {ID: "logs", ExpirationDays: 30, ExpirationDate: "2030-01-01"}, {ID: "logs", ExpirationDays: 30, DeleteMarkers: true}, {ID: "logs", ExpirationDate: "2030-02-30"}, {ID: "logs", Prefix: "a\x00", ExpirationDays: 30}, {ID: strings.Repeat("я", 128), ExpirationDays: 30}}
	for _, input := range invalid {
		if _, err := bucketssvc.ApplyLifecycleInput(lifecycle.Rule{}, input); err == nil {
			t.Fatal("accepted invalid rule", input)
		}
	}
	for _, input := range []bucketssvc.LifecycleInput{base, {ID: "date", ExpirationDate: "2030-02-01"}, {ID: "versions", NoncurrentDays: 90, KeepVersions: 3}, {ID: "count", KeepVersions: 3}, {ID: "markers", DeleteMarkers: true}} {
		if _, err := bucketssvc.ApplyLifecycleInput(lifecycle.Rule{}, input); err != nil {
			t.Fatal(input, err)
		}
	}
}

func TestLifecycleIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("requires isolated docker-compose.test.yml fixture")
	}
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
	app := runtimesvc.New(cfg)
	defer app.Close()
	srv := httptest.NewServer(apihttp.NewHandler(app))
	defer srv.Close()
	root := loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret")
	fixture, _ := minio.New("127.0.0.1:19000", &minio.Options{Creds: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", ""), Region: "us-east-1"})
	admin, _ := madmin.New("127.0.0.1:19000", "minio-web-test", "minio-web-test-secret", false)
	ctx := context.Background()
	suffix := strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:10]))
	bucket, user, policy := "lifecycle-"+suffix, "lifecycle-user-"+suffix, "lifecycle-policy-"+suffix
	defer func() {
		_, _ = admin.DetachPolicy(ctx, madmin.PolicyAssociationReq{User: user, Policies: []string{policy}})
		_ = admin.RemoveUser(ctx, user)
		_ = admin.RemoveCannedPolicy(ctx, policy)
		_ = fixture.RemoveBucket(ctx, bucket)
	}()
	root.call("POST", "admin/buckets", map[string]any{"name": bucket}, 200)
	base := "admin/buckets/" + bucket + "/lifecycle"
	get := func(c *apiClient) presentation.LifecycleSnapshot {
		t.Helper()
		var field struct {
			Value presentation.LifecycleSnapshot `json:"value"`
			Error string                         `json:"error"`
		}
		raw := c.call("GET", base, nil, 200)
		if json.Unmarshal(raw, &field) != nil || field.Error != "" || !field.Value.Writable || field.Value.Revision == "" {
			t.Fatal("unreadable lifecycle", string(raw))
		}
		return field.Value
	}
	current := get(root)
	if len(current.Rules) != 0 {
		t.Fatal(current)
	}
	first := bucketssvc.LifecycleInput{ID: "logs", Prefix: "logs/", ExpirationDays: 30}
	root.call("POST", base, map[string]any{"revision": current.Revision, "rule": first}, 200)
	stale := current.Revision
	current = get(root)
	if len(current.Rules) != 1 || current.Rules[0].Status != "Disabled" {
		t.Fatal(current)
	}
	root.call("PATCH", base, map[string]any{"revision": stale, "index": 0, "enabled": true}, 409)
	root.call("PATCH", base, map[string]any{"revision": current.Revision, "index": 0, "enabled": true}, 200)
	current = get(root)
	if current.Rules[0].Status != "Enabled" {
		t.Fatal(current)
	}
	first.Enabled = true
	first.Prefix = "archive/"
	first.ExpirationDays = 60
	root.call("PUT", base, map[string]any{"revision": current.Revision, "index": 0, "rule": first}, 200)
	current = get(root)
	if current.Rules[0].Rule.Prefix != "archive/" || current.Rules[0].Rule.ExpirationDays != 60 {
		t.Fatal(current)
	}
	root.call("POST", base, map[string]any{"revision": current.Revision, "rule": first}, 409)
	// Add a rule with conditions outside the form, simulating a change through mc.
	external, _ := fixture.GetBucketLifecycle(ctx, bucket)
	external.Rules = append(external.Rules, lifecycle.Rule{ID: "tagged", Status: "Disabled", RuleFilter: lifecycle.Filter{And: lifecycle.And{Prefix: "tagged/", Tags: []lifecycle.Tag{{Key: "env", Value: "dev"}}}}, Expiration: lifecycle.Expiration{Days: 40}})
	if err := fixture.SetBucketLifecycle(ctx, bucket, external); err != nil {
		t.Fatal(err)
	}
	root.call("DELETE", base, map[string]any{"revision": current.Revision, "index": 0, "confirmation": "logs"}, 409)
	current = get(root)
	tagged := current.Rules[1].Rule
	tagged.Prefix = ""
	tagged.ExpirationDays = 50
	root.call("PUT", base, map[string]any{"revision": current.Revision, "index": 1, "rule": tagged}, 200)
	actual, _ := fixture.GetBucketLifecycle(ctx, bucket)
	if len(actual.Rules) != 2 || actual.Rules[1].RuleFilter.Tag.Key != "env" || actual.Rules[1].Expiration.Days != 50 || actual.Rules[0].Expiration.Days != 60 {
		t.Fatal(actual)
	}
	current = get(root)
	root.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": user, "secretKey": "Lifecycle-fixture-secret-123"}, 200)
	doc := json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:ListAllMyBuckets"],"Resource":["arn:aws:s3:::*"]},{"Effect":"Allow","Action":["s3:GetLifecycleConfiguration"],"Resource":["arn:aws:s3:::` + bucket + `"]}]}`)
	root.call("POST", "admin/policies", map[string]any{"acknowledgeNonAtomicCreate": true, "name": policy, "document": doc}, 200)
	root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": policy}, 200)
	limited := loginTest(t, srv.URL, user, "Lifecycle-fixture-secret-123")
	get(limited)
	limited.call("PATCH", base, map[string]any{"revision": current.Revision, "index": 0, "enabled": false}, 403)
	root.call("DELETE", base, map[string]any{"revision": current.Revision, "index": 1, "confirmation": "wrong"}, 400)
	root.call("DELETE", base, map[string]any{"revision": current.Revision, "index": 1, "confirmation": "tagged"}, 200)
	current = get(root)
	// Check the SDK's empty-config DELETE cannot turn an upstream 403 into success.
	limited.call("DELETE", base, map[string]any{"revision": current.Revision, "index": 0, "confirmation": "logs"}, 403)
	if len(get(root).Rules) != 1 {
		t.Fatal("denied delete changed rules")
	}
	root.call("DELETE", base, map[string]any{"revision": current.Revision, "index": 0, "confirmation": "logs"}, 200)
	if len(get(root).Rules) != 0 {
		t.Fatal("last rule not removed")
	}
	// Date, old-version count and marker modes round-trip on actual MinIO.
	for _, rule := range []bucketssvc.LifecycleInput{{ID: "date", ExpirationDate: "2035-01-01"}, {ID: "old", NoncurrentDays: 90, KeepVersions: 3}, {ID: "count", KeepVersions: 3}, {ID: "markers", DeleteMarkers: true}} {
		current = get(root)
		root.call("POST", base, map[string]any{"revision": current.Revision, "rule": rule}, 200)
	}
	if len(get(root).Rules) != 4 {
		t.Fatal("missing action modes")
	}
}

func TestLifecycleUnknownConfigCannotBeWritten(t *testing.T) {
	writes := 0
	source := `<LifecycleConfiguration><Rule><ID>future</ID><Status>Enabled</Status><Filter><Prefix>logs/</Prefix></Filter><Expiration><Days>30</Days></Expiration><FutureSetting><Flag>preserve-me</Flag></FutureSetting></Rule></LifecycleConfiguration>`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(source))
	}))
	defer upstream.Close()
	endpoint, _ := url.Parse(upstream.URL)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	session := testsession.With(t, runtimesvc.Connection{Endpoint: endpoint.String()}, testConfig(), transport)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	current, err := bucketssvc.ReadLifecycle(ctx, runtimesvc.NewClients(session), "fixture-bucket")
	if err != nil || current.View.Writable || messages.Empty(current.View.Warning) {
		t.Fatal(current, err)
	}
	raw, _ := json.Marshal(map[string]any{"revision": current.View.Revision, "index": 0, "enabled": false})
	request := httptest.NewRequest("PATCH", "/", strings.NewReader(string(raw)))
	request.Header.Set("Content-Type", "application/json")
	app := runtimesvc.New(testConfig())
	defer app.Close()
	err = bucketscontroller.HandleChangeBucketLifecycle(app, httptest.NewRecorder(), request, session, "fixture-bucket", "toggle")
	if apperror.Public(err).Code != "LifecycleUnsupported" || writes != 0 {
		t.Fatal("unknown configuration overwritten", err, writes)
	}
}

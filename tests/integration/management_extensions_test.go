package integration

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/cors"
	"github.com/minio/minio-go/v7/pkg/credentials"

	apihttp "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/presentation"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	batchsvc "github.com/owiks/golang-minio-s3-backend-api/src/services/batch"
	bucketssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/buckets"
	objectssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
	serversvc "github.com/owiks/golang-minio-s3-backend-api/src/services/server"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

func TestCORSS3RoundTrip(t *testing.T) {
	var cfg *cors.Config
	deny := false
	writes := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.Method != "GET" {
			writes++
			if deny {
				w.WriteHeader(403)
				fmt.Fprint(w, `<Error><Code>AccessDenied</Code></Error>`)
				return
			}
		}
		switch r.Method {
		case "GET":
			if cfg == nil {
				w.WriteHeader(404)
				fmt.Fprint(w, `<Error><Code>NoSuchCORSConfiguration</Code></Error>`)
			} else {
				raw, _ := xml.Marshal(cfg)
				w.Write(raw)
			}
		case "PUT":
			var c cors.Config
			if xml.NewDecoder(r.Body).Decode(&c) != nil {
				t.Error("bad CORS XML")
			}
			cfg = &c
		case "DELETE":
			cfg = nil
			w.WriteHeader(204)
		}
	}))
	defer upstream.Close()
	endpoint, _ := url.Parse(upstream.URL)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	s := testsession.With(t, runtimesvc.Connection{Endpoint: endpoint.String()}, testConfig(), transport)
	app := runtimesvc.New(testConfig())
	defer app.Close()
	call := func(method string, body any, want int) {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/", strings.NewReader(string(raw)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		err := runHTTPAction(app, w, r, s, "admin/buckets/fixture-bucket/cors")
		status := 200
		if err != nil {
			status = apperror.Public(err).Status
		}
		if status != want {
			t.Fatal(method, status, want, err)
		}
	}
	read := func() bucketssvc.BrowserSettingState {
		t.Helper()
		v, e := bucketssvc.ReadBrowserSetting(context.Background(), runtimesvc.NewClients(s), "fixture-bucket", "cors")
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	first := bucketssvc.CorsRule{ID: "first", Origins: []string{"https://example.com"}, Methods: []string{"GET", "HEAD"}, Headers: []string{"*"}, Expose: []string{"ETag"}, MaxAge: 3600}
	rev := read().Revision
	call("POST", map[string]any{"revision": rev, "rule": first}, 200)
	call("POST", map[string]any{"revision": rev, "rule": first}, 409)
	second := first
	second.ID = "second"
	call("POST", map[string]any{"revision": read().Revision, "rule": second}, 200)
	first.MaxAge = 60
	call("PUT", map[string]any{"revision": read().Revision, "index": 0, "rule": first}, 200)
	if len(cfg.CORSRules) != 2 || cfg.CORSRules[0].MaxAgeSeconds != 60 || cfg.CORSRules[1].ID != "second" {
		t.Fatal(cfg)
	}
	call("DELETE", map[string]any{"revision": read().Revision, "index": 1}, 400)
	call("DELETE", map[string]any{"revision": read().Revision, "index": 1, "confirmation": "delete"}, 200)
	deny = true
	call("DELETE", map[string]any{"revision": read().Revision, "index": 0, "confirmation": "delete"}, 403)
	if cfg == nil {
		t.Fatal("denied DELETE changed config")
	}
	deny = false
	call("DELETE", map[string]any{"revision": read().Revision, "index": 0, "confirmation": "delete"}, 200)
	if cfg != nil || writes != 6 {
		t.Fatal(cfg, writes)
	}
	raw := `<CORSConfiguration><CORSRule><AllowedOrigin>*</AllowedOrigin><AllowedMethod>GET</AllowedMethod><Future>keep</Future></CORSRule></CORSConfiguration>`
	var future cors.Config
	xml.Unmarshal([]byte(raw), &future)
	if bucketssvc.LifecycleRoundTripSafe([]byte(raw), &future) {
		t.Fatal("unknown CORS field lost")
	}
}
func TestDiagnosticProjectionAndBatchValidation(t *testing.T) {
	trace := madmin.TraceInfo{NodeName: "node:9000", FuncName: "s3.GetObject", Path: "https://user:password@example.com/bucket/object?X-Amz-Signature=SECRET", Message: "SECRET", Error: "SECRET", Custom: map[string]string{"secret": "SECRET"}, HTTP: &madmin.TraceHTTPStats{ReqInfo: madmin.TraceRequestInfo{Method: "GET", Path: "/bucket/object?token=SECRET", RawQuery: "secret=SECRET", Headers: http.Header{"Authorization": {"SECRET"}}, Body: []byte("SECRET")}, RespInfo: madmin.TraceResponseInfo{StatusCode: 403, Body: []byte("SECRET")}}}
	raw, _ := json.Marshal(serversvc.TraceRow(trace))
	if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "password") || strings.Contains(string(raw), "token") {
		t.Fatal(string(raw))
	}
	for _, b := range []batchsvc.BatchInput{{Type: "expire", Bucket: "fixture-bucket", Confirmation: "wrong", OlderHours: 1}, {Type: "expire", Bucket: "fixture-bucket", Confirmation: "fixture-bucket", OlderHours: 0}, {Type: "replicate", Bucket: "fixture-bucket", Confirmation: "fixture-bucket"}, {Type: "keyrotate", Bucket: "fixture-bucket", Confirmation: "fixture-bucket", Encryption: "invalid"}} {
		if _, err := batchsvc.BatchDocument(b); err == nil {
			t.Fatal(b)
		}
	}
}
func TestManagementExtensionsIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("isolated fixture")
	}
	ctx := context.Background()
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
	app := runtimesvc.New(cfg)
	defer app.Close()
	srv := httptest.NewServer(apihttp.NewHandler(app))
	defer srv.Close()
	root := loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret")
	client, _ := minio.New("127.0.0.1:19000", &minio.Options{Creds: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", ""), Region: "us-east-1"})
	suffix := strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:10]))
	bucket := "versions-" + suffix
	root.call("POST", "admin/buckets", map[string]any{"name": bucket, "objectLock": true}, 200)
	defer func() {
		for item := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{WithVersions: true, Recursive: true}) {
			if item.Err == nil {
				client.PutObjectLegalHold(ctx, bucket, item.Key, minio.PutObjectLegalHoldOptions{VersionID: item.VersionID, Status: holdPointer(minio.LegalHoldDisabled)})
				client.RemoveObject(ctx, bucket, item.Key, minio.RemoveObjectOptions{VersionID: item.VersionID, GovernanceBypass: true})
			}
		}
		client.RemoveBucket(ctx, bucket)
	}()
	key := "folder/тест + file.txt"
	var firstID string
	for i := 0; i < 55; i++ {
		res, err := client.PutObject(ctx, bucket, key, strings.NewReader(fmt.Sprint(i)), -1, minio.PutObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			firstID = res.VersionID
		}
	}
	client.PutObject(ctx, bucket, key+"-neighbor", strings.NewReader("neighbor"), 8, minio.PutObjectOptions{})
	client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
	q := url.Values{"bucket": {bucket}, "key": {key}}
	var page objectssvc.VersionsPage
	json.Unmarshal(root.call("GET", "admin/object-versions?"+q.Encode(), nil, 200), &page)
	if len(page.Rows) != 50 || page.Next == "" || !page.Rows[0].Deleted || !page.Rows[0].Latest {
		t.Fatal(page)
	}
	q.Set("cursor", page.Next)
	json.Unmarshal(root.call("GET", "admin/object-versions?"+q.Encode(), nil, 200), &page)
	if len(page.Rows) != 6 || page.Next != "" || page.Rows[5].ID != firstID {
		t.Fatal(page)
	}
	q.Del("cursor")
	q.Set("version", firstID)
	base := "admin/object-versions/"
	_, holdErr := client.GetObjectLegalHold(ctx, bucket, key, minio.GetObjectLegalHoldOptions{VersionID: firstID})
	t.Logf("unset hold code: %s", minio.ToErrorResponse(holdErr).Code)
	var fields map[string]presentation.DetailField
	get := func() {
		json.Unmarshal(root.call("GET", base+"protection?"+q.Encode(), nil, 200), &fields)
		if fields["hold"].Error != "" || fields["retention"].Error != "" {
			t.Fatal(fields)
		}
	}
	get()
	rev := fields["hold"].Revision
	root.call("PUT", base+"hold?"+q.Encode(), map[string]any{"revision": rev, "status": "ON", "confirmation": firstID}, 200)
	root.call("PUT", base+"hold?"+q.Encode(), map[string]any{"revision": rev, "status": "OFF", "confirmation": firstID}, 409)
	get()
	root.call("PUT", base+"hold?"+q.Encode(), map[string]any{"revision": fields["hold"].Revision, "status": "OFF", "confirmation": firstID}, 200)
	get()
	until := time.Now().UTC().Add(time.Hour).Truncate(time.Second).Format(time.RFC3339)
	root.call("PUT", base+"retention?"+q.Encode(), map[string]any{"revision": fields["retention"].Revision, "mode": "GOVERNANCE", "until": until, "confirmation": firstID}, 200)
	get()
	t.Logf("protected retention code: %s", minio.ToErrorResponse(client.PutObjectRetention(ctx, bucket, key, minio.PutObjectRetentionOptions{VersionID: firstID})).Code)
	root.call("PUT", base+"retention?"+q.Encode(), map[string]any{"revision": fields["retention"].Revision, "mode": "", "confirmation": firstID}, 403)
	var link struct {
		URL string `json:"url"`
	}
	json.Unmarshal(root.call("GET", base+"url?"+q.Encode(), nil, 200), &link)
	resp, err := http.Get(link.URL)
	if err != nil || resp.StatusCode != 200 {
		t.Fatal(resp, err)
	}
	resp.Body.Close()
	// COMPLIANCE is tested on a short-lived fixture version, then allowed to
	// expire before cleanup. Production retention is never bypassed or shortened.
	complianceUntil := time.Now().UTC().Add(4 * time.Second).Truncate(time.Second)
	comp, err := client.PutObject(ctx, bucket, "compliance", strings.NewReader("fixture"), 7, minio.PutObjectOptions{Mode: minio.Compliance, RetainUntilDate: complianceUntil})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if delay := time.Until(complianceUntil.Add(100 * time.Millisecond)); delay > 0 {
			time.Sleep(delay)
		}
	}()
	compQ := url.Values{"bucket": {bucket}, "key": {"compliance"}, "version": {comp.VersionID}}
	var compFields map[string]presentation.DetailField
	json.Unmarshal(root.call("GET", base+"protection?"+compQ.Encode(), nil, 200), &compFields)
	root.call("PUT", base+"retention?"+compQ.Encode(), map[string]any{"revision": compFields["retention"].Revision, "mode": "", "confirmation": comp.VersionID}, 403)
	// One denied protection field must not hide the other field.
	user, policy := "protection-user-"+suffix, "protection-policy-"+suffix
	admin, _ := madmin.New("127.0.0.1:19000", "minio-web-test", "minio-web-test-secret", false)
	defer func() {
		admin.DetachPolicy(ctx, madmin.PolicyAssociationReq{User: user, Policies: []string{policy}})
		admin.RemoveUser(ctx, user)
		admin.RemoveCannedPolicy(ctx, policy)
	}()
	root.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": user, "secretKey": "Protection-fixture-secret-123"}, 200)
	doc := json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:ListAllMyBuckets","s3:GetObjectRetention"],"Resource":["arn:aws:s3:::*"]}]}`)
	root.call("POST", "admin/policies", map[string]any{"acknowledgeNonAtomicCreate": true, "name": policy, "document": doc}, 200)
	root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": policy}, 200)
	limited := loginTest(t, srv.URL, user, "Protection-fixture-secret-123")
	var independent map[string]presentation.DetailField
	json.Unmarshal(limited.call("GET", base+"protection?"+q.Encode(), nil, 200), &independent)
	if independent["retention"].Revision == "" || independent["hold"].Error == "" {
		t.Fatal(independent)
	}
	// Jobs operate on an empty owned prefix so acceptance tests cannot delete data.
	root.call("GET", "admin/batch-jobs", nil, 200)
	for _, kind := range []string{"expire", "keyrotate"} {
		raw := root.call("POST", "admin/batch-jobs", batchsvc.BatchInput{Type: kind, Bucket: bucket, Prefix: "empty/", Confirmation: bucket, OlderHours: 24, KeepVersions: 1, Encryption: "sse-s3"}, 200)
		var job madmin.BatchJobResult
		json.Unmarshal(raw, &job)
		if job.ID == "" {
			t.Fatal(string(raw))
		}
		time.Sleep(100 * time.Millisecond)
		root.call("GET", "admin/batch-jobs/"+job.ID, nil, 200)
		var parameters batchsvc.BatchParameters
		json.Unmarshal(root.call("GET", "admin/batch-jobs/"+job.ID+"/parameters", nil, 200), &parameters)
		if !parameters.Supported || parameters.Bucket != bucket || parameters.Type != kind {
			t.Fatalf("unexpected job parameters: %+v", parameters)
		}
		root.call("DELETE", "admin/batch-jobs/"+job.ID, map[string]any{"confirmation": "wrong"}, 400)
		status, _ := root.request("DELETE", root.Base+"/admin/batch-jobs/"+job.ID, map[string]any{"confirmation": job.ID})
		if status != 200 && status != 404 {
			t.Fatal("cancel", status)
		}
	}
	root.call("GET", "admin/diagnostics?kind=logs&seconds=1&limit=10&level=ALL", nil, 200)
	root.call("GET", "admin/diagnostics?kind=trace&seconds=11&limit=10&level=ALL", nil, 400)
	root.call("GET", "admin/diagnostics?kind=trace&seconds=1&limit=10&level=ALL", nil, 200)
}
func holdPointer(v minio.LegalHoldStatus) *minio.LegalHoldStatus { return &v }

func TestExtensionPermissionsIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("isolated fixture")
	}
	ctx := context.Background()
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
	app := runtimesvc.New(cfg)
	defer app.Close()
	srv := httptest.NewServer(apihttp.NewHandler(app))
	defer srv.Close()
	root := loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret")
	admin, _ := madmin.New("127.0.0.1:19000", "minio-web-test", "minio-web-test-secret", false)
	suffix := strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:10]))
	bucket, user, policy := "extension-"+suffix, "extension-user-"+suffix, "extension-policy-"+suffix
	defer func() {
		admin.DetachPolicy(ctx, madmin.PolicyAssociationReq{User: user, Policies: []string{policy}})
		admin.RemoveUser(ctx, user)
		admin.RemoveCannedPolicy(ctx, policy)
		client, _ := minio.New("127.0.0.1:19000", &minio.Options{Creds: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", ""), Region: "us-east-1"})
		client.RemoveBucket(ctx, bucket)
	}()
	root.call("POST", "admin/buckets", map[string]any{"name": bucket}, 200)
	root.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": user, "secretKey": "Extension-fixture-secret-123"}, 200)
	doc := json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:ListAllMyBuckets"],"Resource":["arn:aws:s3:::*"]},{"Effect":"Allow","Action":["s3:GetEncryptionConfiguration"],"Resource":["arn:aws:s3:::` + bucket + `"]}]}`)
	root.call("POST", "admin/policies", map[string]any{"acknowledgeNonAtomicCreate": true, "name": policy, "document": doc}, 200)
	root.call("POST", "admin/bindings", map[string]any{"user": user, "policy": policy}, 200)
	limited := loginTest(t, srv.URL, user, "Extension-fixture-secret-123")
	for _, kind := range []string{"encryption"} {
		base := "admin/buckets/" + bucket + "/" + kind
		var field presentation.DetailField
		json.Unmarshal(limited.call("GET", base, nil, 200), &field)
		if kind == "cors" {
			limited.call("POST", base, map[string]any{"revision": field.Revision, "rule": bucketssvc.CorsRule{Origins: []string{"*"}, Methods: []string{"GET"}}}, 403)
		} else {
			limited.call("PUT", base, map[string]any{"revision": field.Revision, "algorithm": "AES256"}, 403)
		}
	}
	limited.call("GET", "admin/object-versions?bucket="+bucket+"&key=file", nil, 403)
	limited.call("GET", "admin/batch-jobs", nil, 403)
	limited.call("POST", "admin/batch-jobs", batchsvc.BatchInput{Type: "expire", Bucket: bucket, Prefix: "empty/", OlderHours: 24, Confirmation: bucket}, 403)
	limited.call("GET", "admin/diagnostics?kind=logs&seconds=1&limit=10&level=ALL", nil, 403)
	// Native trace permission denial must remain a 403 even when the SDK closes
	// its failing response body before decoding the original error.
	status, _ := limited.request("GET", limited.Base+"/admin/diagnostics?kind=trace&seconds=1&limit=10&level=ALL", nil)
	if status != 403 {
		t.Fatal(status)
	}
}

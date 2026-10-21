package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minio/madmin-go/v3"

	batchcontroller "minio-web/api/src/http/api/routes/batch/controllers"
	"minio-web/api/src/services/apperror"
	batchsvc "minio-web/api/src/services/batch"
	runtimesvc "minio-web/api/src/services/runtime"
	"minio-web/api/tests/testsession"
)

func TestSafeBatchParameters(t *testing.T) {
	for _, kind := range []string{"expire", "keyrotate"} {
		doc, err := batchsvc.BatchDocument(batchsvc.BatchInput{Type: kind, Bucket: "fixture-bucket", Prefix: "daily/", OlderHours: 168, KeepVersions: 0, Encryption: "sse-kms", KeyID: "backup-key", Confirmation: "fixture-bucket"})
		if err != nil {
			t.Fatal(err)
		}
		v, err := batchsvc.SafeBatchParameters(doc)
		if err != nil || !v.Supported || v.Partial || v.Bucket != "fixture-bucket" || len(v.Prefixes) != 1 || v.Prefixes[0] != "daily/" {
			t.Fatalf("%+v %v", v, err)
		}
		if kind == "expire" && (len(v.Rules) != 1 || v.Rules[0].OlderThan != "168h0m0s" || v.Rules[0].RetainVersions == nil || *v.Rules[0].RetainVersions != 0) {
			t.Fatal(v)
		}
		if kind == "keyrotate" && (v.Encryption != "sse-kms" || v.KeyID != "backup-key") {
			t.Fatal(v)
		}
	}
	secretDoc := `expire:
  apiVersion: v1
  bucket: fixture-bucket
  prefix: [daily/, weekly/]
  notify:
    endpoint: https://SECRET@example.invalid
    token: SECRET
  rules:
    - type: object
      name: '*'
      olderThan: 24h
      tags: [{key: internal, value: SECRET}]
      purge: {retainVersions: 2}
`
	v, err := batchsvc.SafeBatchParameters(secretDoc)
	if err != nil || !v.Partial || len(v.Prefixes) != 2 {
		t.Fatalf("%+v %v", v, err)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "notify") {
		t.Fatal(string(raw))
	}
	for _, kind := range []string{"replicate", "catalog"} {
		v, err = batchsvc.SafeBatchParameters(kind + ":\n  credentials: SECRET\n")
		raw, _ = json.Marshal(v)
		if err != nil || v.Supported || !v.Partial || v.Type != kind || strings.Contains(string(raw), "SECRET") {
			t.Fatal(v, err)
		}
	}
	for _, doc := range []string{"expire: [SECRET", "- SECRET", "expire: {prefix: {password: SECRET}, rules: []}", "expire: {}\nkeyrotate: {}", "expire: {rules: [{purge: {retainVersions: SECRET}}]}", strings.Repeat("SECRET", 200000)} {
		_, err = batchsvc.SafeBatchParameters(doc)
		if err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatal("unsafe error", err)
		}
	}
}

func TestBatchParametersSDKAndDenied(t *testing.T) {
	denied := false
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/minio/admin/v3/describe-job" || r.URL.Query().Get("jobId") != "job-one" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		if denied {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(403)
			fmt.Fprint(w, `{"Code":"AccessDenied","Message":"Access denied"}`)
			return
		}
		fmt.Fprint(w, `expire: {apiVersion: v1, bucket: fixture-bucket, prefix: '', rules: [{type: object, name: '*', olderThan: 24h, purge: {retainVersions: 1}}], notify: {token: SECRET}}`)
	}))
	defer upstream.Close()
	app := runtimesvc.New(testConfig())
	defer app.Close()
	s := testsession.New(t, upstream.URL)
	w := httptest.NewRecorder()
	err := batchcontroller.HandleBatchParameters(app, w, httptest.NewRequest("GET", "/", nil), s, "job-one")
	if err != nil || w.Code != 200 || strings.Contains(w.Body.String(), "SECRET") || !strings.Contains(w.Body.String(), `"bucket":"fixture-bucket"`) {
		t.Fatal(w.Body.String(), err)
	}
	denied = true
	w = httptest.NewRecorder()
	err = batchcontroller.HandleBatchParameters(app, w, httptest.NewRequest("GET", "/", nil), s, "job-one")
	if err == nil || apperror.Public(err).Status != 403 || w.Body.Len() != 0 || calls != 2 {
		t.Fatal(calls, err, w.Body.String())
	}
}

func TestBatchMetricProjection(t *testing.T) {
	v := batchsvc.SafeBatchMetric(madmin.JobMetric{JobID: "job", Complete: true, Catalog: &madmin.CatalogInfo{ObjectsScannedCount: 12, ErrorMsg: "SECRET"}})
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "SECRET") || !strings.Contains(string(raw), `"objectsScannedCount":12`) || !strings.Contains(string(raw), `"complete":true`) {
		t.Fatal(string(raw))
	}
}

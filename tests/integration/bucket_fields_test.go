package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	bucketscontroller "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/buckets/controllers"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

func TestBucketDetailsSelectedFields(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !r.URL.Query().Has("policy") {
			t.Errorf("unrelated field requested: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Version":"2012-10-17","Statement":[]}`))
	}))
	defer upstream.Close()
	client, err := minio.New(strings.TrimPrefix(upstream.URL, "http://"), &minio.Options{Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	err = bucketscontroller.HandleBucketDetails((&runtimesvc.State{}), response, httptest.NewRequest("GET", "/?fields=policy", nil), testsession.New(t, client.EndpointURL().String()), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	_ = json.Unmarshal(response.Body.Bytes(), &result)
	if calls != 1 || len(result) != 2 || result["policy"] == nil {
		t.Fatal(calls, result)
	}
	response = httptest.NewRecorder()
	err = bucketscontroller.HandleBucketDetails((&runtimesvc.State{}), response, httptest.NewRequest("GET", "/?fields=unknown", nil), testsession.New(t, client.EndpointURL().String()), "fixture")
	if err == nil || calls != 1 {
		t.Fatal("invalid field was read", err, calls)
	}
}

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/iam"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func TestActionAccountIgnoresUnrelatedVersionDependentFields(t *testing.T) {
	s := actionCheckSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/minio/admin/v3/accountinfo" {
			t.Errorf("unexpected account request %s %s", r.Method, r.URL.Path)
		}
		// These fields cannot decode into the pinned madmin.AccountInfo, but do not
		// participate in identity or IAM decisions. Their values must be ignored.
		fmt.Fprintf(w, `{"AccountName":"fixture-user","Policy":%s,"Server":{"StandardSCData":"future-format"},"Buckets":[{"created":"not-a-date","details":{"tags":false,"quota":123}}]}`, actionCheckPolicy)
	})
	_, legacyError := runtimesvc.NewClients(s).AccountInfo(context.Background(), madmin.AccountOpts{})
	if legacyError == nil {
		t.Fatal("fixture must reproduce full SDK decoding failure")
	}
	issue := s.RefreshActionPolicy(context.Background())
	if issue != nil || !s.Actions().Known() || !s.Actions().Allows(iam.Query{Action: "s3:PutObject", Bucket: "data", Key: "file.txt"}) {
		t.Fatalf("unexpected policy: issue=%v", issue)
	}
}
func TestActionAccountRejectsInvalidIdentityAndProxyResponses(t *testing.T) {
	for _, tc := range []struct {
		Name              string `json:"-"`
		Status            int    `json:"-"`
		Body, Code, Cause string `json:"-"`
	}{
		{"html proxy", 200, `<html>SECRET-PROXY</html>`, "AccountInfoInvalidResponse", "invalid-json"},
		{"identity type", 200, `{"AccountName":{"SECRET":"user"},"Policy":{}}`, "AccountInfoInvalidResponse", "invalid-json"},
		{"forbidden proxy", 403, `SECRET-PROXY`, "AccountInfoDenied", "access-denied"},
		{"route proxy", 404, `SECRET-PROXY`, "AccountInfoRouteUnavailable", "route-unavailable"},
		{"method proxy", 405, `SECRET-PROXY`, "AccountInfoRouteUnavailable", "route-unavailable"},
		{"server error", 500, `{"Code":"FixtureServerError","Message":"SECRET-UPSTREAM"}`, "AccountInfoServerError", "server-error"},
		{"unsupported", 501, `{"Code":"NotImplemented","Message":"SECRET-UPSTREAM"}`, "AccountInfoUnsupported", "unsupported"},
		{"oversized", 200, strings.Repeat("x", runtimesvc.MaxActionAccountResponse+1), "AccountInfoTooLarge", "response-too-large"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			s := actionCheckSession(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.Status); fmt.Fprint(w, tc.Body) })
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			issue := s.RefreshActionPolicy(ctx)
			if issue == nil || issue.Code != tc.Code || issue.Cause != tc.Cause || issue.HTTPStatus != tc.Status || s.Actions().Known() {
				t.Fatalf("issue=%+v", issue)
			}
			raw, _ := json.Marshal(issue)
			if bytes.Contains(raw, []byte("SECRET")) {
				t.Fatal("upstream response exposed")
			}
		})
	}
}

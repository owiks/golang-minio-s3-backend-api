package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	httpcommon "minio-web/api/src/http/api/common"
	accesscontroller "minio-web/api/src/http/api/routes/access/controllers"
	accesssvc "minio-web/api/src/services/access"
	"minio-web/api/src/services/iam"
	runtimesvc "minio-web/api/src/services/runtime"
	"minio-web/api/tests/testsession"
)

var actionCheckPolicy = json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:*"],"Resource":["*"]},{"Effect":"Allow","Action":["s3:*"],"Resource":["arn:aws:s3:::*"]}]}`)

func actionCheckSession(t *testing.T, handler http.HandlerFunc) *runtimesvc.Session {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	u, _ := url.Parse(upstream.URL)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(tr.CloseIdleConnections)
	return testsession.With(t, runtimesvc.Connection{Endpoint: u.String(), AccessKey: "fixture-user", SecretKey: "public-fixture-secret"}, testConfig(), tr)
}
func TestActionCheckRunsBeforeInventoryCanExhaustDeadline(t *testing.T) {
	var accounts atomic.Int32
	both := make(chan struct{})
	s := actionCheckSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("discovery wrote to MinIO")
			w.WriteHeader(405)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/accountinfo") {
			if accounts.Add(1) == 2 {
				close(both)
			}
			httpcommon.WriteJSON(w, 200, map[string]any{"AccountName": "fixture-user", "Policy": actionCheckPolicy, "Buckets": []map[string]any{{"name": "fixture-bucket", "access": map[string]bool{"read": true}}}})
			return
		}
		if r.URL.Path == "/" {
			select {
			case <-both:
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<ListAllMyBucketsResult><Buckets><Bucket><Name>fixture-bucket</Name></Bucket></Buckets></ListAllMyBucketsResult>`)
			return
		}
		httpcommon.WriteJSON(w, 501, map[string]string{"Code": "NotImplemented"})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	buckets, access, err := accesssvc.DiscoverAccess(ctx, runtimesvc.NewClients(s))
	if err != nil || len(buckets) != 1 || slices.Contains(access.Unknown, "actions") || access.ActionCheck != nil {
		t.Fatalf("action policy starved behind inventory: buckets=%v unknown=%v issue=%v err=%v", buckets, access.Unknown, access.ActionCheck, err)
	}
	if !s.Actions().Allows(iam.Query{Action: "admin:CreateUser"}) {
		t.Fatal("early policy result missing")
	}
}
func TestActionCheckReasonsRemainClosedAndDoNotExposeUpstreamData(t *testing.T) {
	for _, tc := range []struct {
		Name, Owner string          `json:"-"`
		Policy      json.RawMessage `json:"-"`
		Status      int             `json:"-"`
		Want        string          `json:"-"`
	}{
		{"identity missing", "", actionCheckPolicy, 200, "AccountIdentityMissing"},
		{"policy invalid", "fixture-user", json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:SECRET-UNSUPPORTED-ACTION"],"Resource":["*"]}]}`), 200, "AccountPolicyInvalid"},
		{"account denied", "fixture-user", actionCheckPolicy, 403, "AccountInfoDenied"},
		{"derived key denied", "parent-user", actionCheckPolicy, 200, "AccessKeyInfoDenied"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			var fail atomic.Bool
			s := actionCheckSession(t, func(w http.ResponseWriter, r *http.Request) {
				if !fail.Load() {
					httpcommon.WriteJSON(w, 200, map[string]any{"AccountName": "fixture-user", "Policy": actionCheckPolicy})
					return
				}
				if strings.HasSuffix(r.URL.Path, "/accountinfo") && tc.Status == 200 {
					httpcommon.WriteJSON(w, 200, map[string]any{"AccountName": tc.Owner, "Policy": tc.Policy})
					return
				}
				httpcommon.WriteJSON(w, 403, map[string]string{"Code": "AccessDenied", "Message": "SECRET-UPSTREAM-DATA"})
			})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if issue := s.RefreshActionPolicy(ctx); issue != nil || !s.Actions().Known() {
				t.Fatal("cannot seed known policy", issue)
			}
			fail.Store(true)
			issue := s.RefreshActionPolicy(ctx)
			if issue == nil || issue.Code != tc.Want || s.Actions().Known() {
				t.Fatalf("issue=%v known=%v", issue, s.Actions().Known())
			}
			req := httptest.NewRequest("POST", "/action-access", strings.NewReader(`{"queries":[{"action":"admin:CreateUser"}]}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			app := runtimesvc.New(testConfig())
			defer app.Close()
			if err := accesscontroller.HandleActionAccess(app, w, req, s); err != nil {
				t.Fatal(err)
			}
			var reply struct {
				Allowed []bool
				Known   bool
				Issue   *iam.ActionCheckIssue
			}
			if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
				t.Fatal(err)
			}
			if reply.Known || len(reply.Allowed) != 1 || reply.Allowed[0] || reply.Issue == nil || reply.Issue.Code != tc.Want || bytes.Contains(w.Body.Bytes(), []byte("SECRET")) {
				t.Fatal("unknown rights expanded or upstream details exposed", w.Body.String())
			}
		})
	}
}
func TestActionCheckTimeoutIsExplicit(t *testing.T) {
	s := actionCheckSession(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	issue := s.RefreshActionPolicy(ctx)
	if issue == nil || issue.Code != "ActionCheckTimeout" || s.Actions().Known() {
		t.Fatal(issue)
	}
}

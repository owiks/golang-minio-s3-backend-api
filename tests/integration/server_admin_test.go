package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	apihttp "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	servercontroller "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/server/controllers"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/presentation"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
	serversvc "github.com/owiks/golang-minio-s3-backend-api/src/services/server"
)

func TestServerConfigAllowlistEnvAndHistory(t *testing.T) {
	fields, e := serversvc.ConfigFields("# MINIO_API_REQUESTS_MAX=120\napi requests_max=0 root_access=on cors_allow_origin=secret-value\n", "api")
	if e != nil || len(fields) != 1 || fields[0].Env != "MINIO_API_REQUESTS_MAX" || fields[0].Effective != "120" {
		t.Fatalf("fields=%+v error=%v", fields, e)
	}
	h := serversvc.ConfigHistory([]madmin.ConfigHistoryEntry{{RestoreID: "good", Data: "scanner speed=slow"}, {RestoreID: "secret", Data: "identity_openid client_secret=forbidden"}, {RestoreID: "mixed", Data: "api requests_max=10 root_access=off"}, {RestoreID: "quoted", Data: "api requests_max=\"50\""}}, "api")
	raw, _ := json.Marshal(h)
	if len(h) != 1 || strings.Contains(string(raw), "forbidden") || strings.Contains(string(raw), "root_access") {
		t.Fatal(string(raw))
	}
	for _, value := range []string{"-1", "1000001", "0 root_access=off", "\"1\"", "1\nscanner speed=fast"} {
		if serversvc.ConfigValueValid(fields[0], value) {
			t.Fatal("accepted unsafe value", value)
		}
	}
}
func TestLiveMetricsNullsHistogramsAndWhitelist(t *testing.T) {
	text := `# TYPE minio_s3_requests_total counter
minio_s3_requests_total{api="GetObject"} 12
# TYPE minio_s3_requests_duration_seconds histogram
minio_s3_requests_duration_seconds_bucket{api="GetObject",le="+Inf"} 2
minio_s3_requests_duration_seconds_sum{api="GetObject"} 0.4
minio_s3_requests_duration_seconds_count{api="GetObject"} 2
# TYPE minio_s3_traffic_sent_bytes counter
minio_s3_traffic_sent_bytes 512
# TYPE secret_token gauge
secret_token{password="forbidden"} 123
`
	p := expfmt.NewTextParser(model.UTF8Validation)
	families, e := p.TextToMetricFamilies(strings.NewReader(text))
	if e != nil {
		t.Fatal(e)
	}
	v := serversvc.LiveMetricRows(families)
	if len(v.APIs) != 1 || *v.APIs[0].Requests != 12 || v.APIs[0].Errors4xx != nil || *v.APIs[0].DurationCount != 2 || *v.Output != 512 || v.Input != nil {
		t.Fatalf("%+v", v)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "forbidden") {
		t.Fatal(string(raw))
	}
}
func TestPoolsSnapshotAndMutationGuards(t *testing.T) {
	if serversvc.RebalanceActive("None") || serversvc.RebalanceActive("Completed") || !serversvc.RebalanceActive("Started") {
		t.Fatal("incorrect native rebalance states")
	}
	writes := 0
	topology := "pool-A"
	permissions := actionCheckPolicy
	s := actionCheckSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/accountinfo"):
			httpcommon.WriteJSON(w, r, 200, map[string]any{"AccountName": "fixture-user", "Policy": permissions})
		case strings.HasSuffix(r.URL.Path, "/pools/list"):
			httpcommon.WriteJSON(w, r, 200, []madmin.PoolStatus{{ID: 0, CmdLine: topology}, {ID: 1, CmdLine: "pool-B"}})
		case strings.HasSuffix(r.URL.Path, "/rebalance/status"):
			httpcommon.WriteJSON(w, r, 200, madmin.RebalanceStatus{})
		case strings.HasSuffix(r.URL.Path, "/info"):
			httpcommon.WriteJSON(w, r, 200, madmin.InfoMessage{Servers: []madmin.ServerProperties{{Endpoint: "node", State: "offline"}}})
		default:
			writes++
			httpcommon.WriteJSON(w, r, 200, map[string]bool{"ok": true})
		}
	})
	v, _, e := serversvc.ReadPools(context.Background(), runtimesvc.NewClients(s))
	if e != nil || !v.RebalanceKnown || len(v.Pools) != 2 {
		t.Fatal(v, e)
	}
	app := runtimesvc.New(testConfig())
	defer app.Close()
	call := func(body any, want int) {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/", bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		e := servercontroller.HandleChangeServerPools(app, w, r, s)
		if e == nil || apperror.Public(e).Status != want {
			t.Fatalf("want %d got %v response %s", want, e, w.Body.String())
		}
	}
	call(map[string]any{"operation": "decommission", "pool": 1, "revision": v.Revision, "confirmation": "wrong"}, 400)
	topology = "replacement-pool"
	call(map[string]any{"operation": "decommission", "pool": 1, "revision": v.Revision, "confirmation": "ПУЛ 1"}, 409)
	fresh, _, _ := serversvc.ReadPools(context.Background(), runtimesvc.NewClients(s))
	call(map[string]any{"operation": "decommission", "pool": 1, "revision": fresh.Revision, "confirmation": "ПУЛ 1"}, 409)
	call(map[string]any{"operation": "stop", "revision": fresh.Revision, "confirmation": "ОСТАНОВИТЬ"}, 409)
	permissions = json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:ServerInfo"],"Resource":["*"]}]}`)
	call(map[string]any{"operation": "start", "revision": fresh.Revision, "confirmation": "ПЕРЕРАСПРЕДЕЛИТЬ"}, 403)
	if writes != 0 {
		t.Fatal("unsafe mutations reached MinIO", writes)
	}
}

func TestPoolsTerminalFlagsWithoutStartTime(t *testing.T) {
	for _, state := range []string{"failed", "canceled", "complete"} {
		t.Run(state, func(t *testing.T) {
			s := actionCheckSession(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/pools/list") {
					httpcommon.WriteJSON(w, r, 200, []madmin.PoolStatus{{ID: 0, Decommission: &madmin.PoolDecommissionInfo{Failed: state == "failed", Canceled: state == "canceled", Complete: state == "complete", ObjectsDecommissionFailed: 1}}})
				} else {
					httpcommon.WriteJSON(w, r, 200, madmin.RebalanceStatus{})
				}
			})
			view, _, err := serversvc.ReadPools(context.Background(), runtimesvc.NewClients(s))
			if err != nil || len(view.Pools) != 1 || view.Pools[0].State != state || view.Pools[0].Failed != 1 {
				t.Fatalf("terminal status hidden: %+v %v", view, err)
			}
		})
	}
}

func TestServerConfigRejectsInjectionEnvUnknownAndStaleBeforeWrite(t *testing.T) {
	writes := 0
	raw := "api requests_max=0 cluster_deadline=10s\n"
	s := actionCheckSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/accountinfo"):
			httpcommon.WriteJSON(w, r, 200, map[string]any{"AccountName": "fixture-user", "Policy": actionCheckPolicy})
		case strings.HasSuffix(r.URL.Path, "/get-config-kv"):
			data, _ := madmin.EncryptData("public-fixture-secret", []byte(raw))
			w.Write(data)
		default:
			writes++
			httpcommon.WriteJSON(w, r, 200, map[string]bool{"ok": true})
		}
	})
	app := runtimesvc.New(testConfig())
	defer app.Close()
	current, e := serversvc.ReadServerConfig(context.Background(), runtimesvc.NewClients(s), "api")
	if e != nil {
		t.Fatal(e)
	}
	call := func(rev string, values map[string]string, want int) {
		body, _ := json.Marshal(map[string]any{"revision": rev, "values": values})
		r := httptest.NewRequest("PUT", "/?subsystem=api", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		e := servercontroller.HandleSaveServerConfig(app, httptest.NewRecorder(), r, s)
		if e == nil || apperror.Public(e).Status != want {
			t.Fatalf("expected %d got %v", want, e)
		}
	}
	call(current.Revision, map[string]string{"root_access": "off"}, 400)
	call(current.Revision, map[string]string{"requests_max": "0 root_access=off"}, 400)
	call("stale", map[string]string{"requests_max": "50"}, 409)
	raw = "# MINIO_API_REQUESTS_MAX=100\napi requests_max=0 cluster_deadline=10s\n"
	current, _ = serversvc.ReadServerConfig(context.Background(), runtimesvc.NewClients(s), "api")
	call(current.Revision, map[string]string{"requests_max": "50"}, 409)
	if writes != 0 {
		t.Fatal("invalid writes reached MinIO", writes)
	}
}

func TestServerConfigRestoreNativeIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("isolated fixture")
	}
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
	app := runtimesvc.New(cfg)
	defer app.Close()
	srv := httptest.NewServer(apihttp.NewHandler(app))
	defer srv.Close()
	c := loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret")
	read := func() presentation.ServerConfigView {
		t.Helper()
		var view presentation.ServerConfigView
		if err := json.Unmarshal(c.call("GET", "admin/server-config?subsystem=scanner", nil, 200), &view); err != nil {
			t.Fatal(err)
		}
		return view
	}
	speed := func(view presentation.ServerConfigView) string {
		t.Helper()
		for _, field := range view.Fields {
			if field.Key == "speed" {
				return field.Value
			}
		}
		t.Fatal("scanner speed unavailable")
		return ""
	}
	initial := read()
	original := speed(initial)
	restored := false
	defer func() {
		if restored {
			return
		}
		// Failure cleanup is restricted to this disposable fixture.
		admin, err := madmin.New("127.0.0.1:19000", "minio-web-test", "minio-web-test-secret", false)
		if err != nil {
			t.Error(err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.SetConfigKV(ctx, "scanner speed="+original); err != nil {
			t.Error("restore fixture scanner", err)
		}
	}()
	c.call("PUT", "admin/server-config?subsystem=scanner", serversvc.ConfigChangeRequest{Revision: initial.Revision, Values: map[string]string{"speed": "slow"}}, 200)
	slow := read()
	if speed(slow) != "slow" {
		t.Fatal("configuration was not saved")
	}
	restoreID := ""
	for _, entry := range slow.History {
		if entry.Values["speed"] == "slow" {
			restoreID = entry.ID
			break
		}
	}
	if restoreID == "" {
		t.Fatal("saved configuration missing from native history")
	}
	c.call("PUT", "admin/server-config?subsystem=scanner", serversvc.ConfigChangeRequest{Revision: slow.Revision, Values: map[string]string{"speed": "fast"}}, 200)
	c.call("PUT", "admin/server-config?subsystem=scanner", serversvc.ConfigChangeRequest{Revision: slow.Revision, RestoreID: restoreID}, 409)
	fresh := read()
	if speed(fresh) != "fast" {
		t.Fatal("stale history restore changed current configuration")
	}
	c.call("PUT", "admin/server-config?subsystem=scanner", serversvc.ConfigChangeRequest{Revision: fresh.Revision, RestoreID: restoreID}, 200)
	fresh = read()
	if speed(fresh) != "slow" {
		t.Fatal("native history restore failed")
	}
	c.call("PUT", "admin/server-config?subsystem=scanner", serversvc.ConfigChangeRequest{Revision: fresh.Revision, Values: map[string]string{"speed": original}}, 200)
	if speed(read()) != original {
		t.Fatal("original scanner configuration not restored")
	}
	restored = true
}

// Run separately and last: completing decommission changes the disposable
// fixture topology. Start with docker-compose.test.yml --profile pools --force-recreate.
func TestServerPoolDecommissionNativeIntegration(t *testing.T) {
	if os.Getenv("MINIO_POOL_DECOMMISSION") != "1" {
		t.Skip("dedicated disposable two-pool fixture")
	}
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19002"] = true
	app := runtimesvc.New(cfg)
	defer app.Close()
	srv := httptest.NewServer(apihttp.NewHandler(app))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	c := &apiClient{T: t, Http: &http.Client{Jar: jar, Timeout: 30 * time.Second}, Origin: srv.URL}
	status, reply := c.request("POST", "/api/sessions", runtimesvc.Connection{Endpoint: "http://127.0.0.1:19002", AccessKey: "minio-web-test", SecretKey: "minio-web-test-secret", Region: "us-east-1"})
	if status != 201 {
		t.Fatal(status, string(reply))
	}
	var login struct{ ID, CSRF string }
	json.Unmarshal(reply, &login)
	c.Base = "/api/sessions/" + login.ID
	c.CSRF = login.CSRF
	client, _ := minio.New("127.0.0.1:19002", &minio.Options{Creds: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", "")})
	ctx := context.Background()
	bucket := "pool-move-" + strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:8]))
	payload := bytes.Repeat([]byte("pool-transfer-fixture-"), 4096)
	if e := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); e != nil {
		t.Fatal(e)
	}
	defer func() {
		for i := range 32 {
			client.RemoveObject(ctx, bucket, fmt.Sprintf("object-%02d", i), minio.RemoveObjectOptions{})
		}
		client.RemoveBucket(ctx, bucket)
	}()
	for i := range 32 {
		if _, e := client.PutObject(ctx, bucket, fmt.Sprintf("object-%02d", i), bytes.NewReader(payload), int64(len(payload)), minio.PutObjectOptions{}); e != nil {
			t.Fatal(e)
		}
	}
	var view presentation.PoolsView
	json.Unmarshal(c.call("GET", "admin/server-pools", nil, 200), &view)
	if view.Rebalancing {
		c.call("POST", "admin/server-pools", map[string]any{"operation": "stop", "revision": view.Revision, "confirmation": "ОСТАНОВИТЬ"}, 200)
		json.Unmarshal(c.call("GET", "admin/server-pools", nil, 200), &view)
	}
	c.call("POST", "admin/server-pools", map[string]any{"operation": "decommission", "pool": 2, "revision": view.Revision, "confirmation": "ПУЛ 2"}, 200)
	deadline := time.Now().Add(25 * time.Second)
	for {
		json.Unmarshal(c.call("GET", "admin/server-pools", nil, 200), &view)
		complete := false
		for _, p := range view.Pools {
			if p.ID == 2 && p.State == "complete" {
				complete = true
			}
		}
		if complete {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("decommission did not complete: %+v", view)
		}
		time.Sleep(time.Second)
	}
	c.call("POST", "admin/server-pools", map[string]any{"operation": "decommission", "pool": 2, "revision": view.Revision, "confirmation": "ПУЛ 2"}, 409)
	for i := range 32 {
		obj, e := client.GetObject(ctx, bucket, fmt.Sprintf("object-%02d", i), minio.GetObjectOptions{})
		if e != nil {
			t.Fatal(e)
		}
		data, e := io.ReadAll(obj)
		obj.Close()
		if e != nil || !bytes.Equal(data, payload) {
			t.Fatalf("object %d changed after decommission: %v", i, e)
		}
	}
}

func TestServerAdminLeastPrivilegeIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("isolated fixture")
	}
	ctx := context.Background()
	admin, _ := madmin.New("127.0.0.1:19000", "minio-web-test", "minio-web-test-secret", false)
	suffix := strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:10]))
	user := "metrics-user-" + suffix
	policy := "metrics-policy-" + suffix
	secret := "fixture-only-secret-123"
	if e := admin.AddUser(ctx, user, secret); e != nil {
		t.Fatal(e)
	}
	defer admin.RemoveUser(ctx, user)
	raw := []byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:Prometheus"],"Resource":["*"]}]}`)
	if e := admin.AddCannedPolicy(ctx, policy, raw); e != nil {
		t.Fatal(e)
	}
	defer admin.RemoveCannedPolicy(ctx, policy)
	if _, e := admin.AttachPolicy(ctx, madmin.PolicyAssociationReq{Policies: []string{policy}, User: user}); e != nil {
		t.Fatal(e)
	}
	defer admin.DetachPolicy(ctx, madmin.PolicyAssociationReq{Policies: []string{policy}, User: user})
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
	app := runtimesvc.New(cfg)
	defer app.Close()
	srv := httptest.NewServer(apihttp.NewHandler(app))
	defer srv.Close()
	c := loginTest(t, srv.URL, user, secret)
	var access struct {
		Access presentation.NavigationAccess `json:"access"`
	}
	json.Unmarshal(c.call("GET", "access", nil, 200), &access)
	hasMetrics := false
	for _, section := range access.Access.Sections {
		if section == "server-config" || section == "server-pools" {
			t.Fatal("overbroad section", section)
		}
		if section == "server-metrics" {
			hasMetrics = true
		}
	}
	if !hasMetrics {
		t.Fatal("metrics permission not discovered", access.Access)
	}
	c.call("GET", "admin/server-metrics", nil, 200)
	c.call("GET", "admin/server-config?subsystem=api", nil, 403)
	c.call("GET", "admin/server-pools", nil, 403)
	c.call("PUT", "admin/server-config?subsystem=api", map[string]any{"revision": "any", "values": map[string]string{"requests_max": "50"}}, 403)
	c.call("POST", "admin/server-pools", map[string]any{"operation": "start", "revision": "any", "confirmation": "ПЕРЕРАСПРЕДЕЛИТЬ"}, 403)
}
func TestServerAdminNativeIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("isolated fixtures")
	}
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
	cfg.Endpoints.Allowed["http://127.0.0.1:19002"] = true
	app := runtimesvc.New(cfg)
	defer app.Close()
	srv := httptest.NewServer(apihttp.NewHandler(app))
	defer srv.Close()
	c := loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret")
	var conf presentation.ServerConfigView
	json.Unmarshal(c.call("GET", "admin/server-config?subsystem=scanner", nil, 200), &conf)
	if len(conf.Fields) == 0 {
		t.Fatal("no scanner fields")
	}
	original := conf.Fields[0].Value
	c.call("PUT", "admin/server-config?subsystem=scanner", map[string]any{"revision": conf.Revision, "values": map[string]string{"speed": "slow"}}, 200)
	c.call("PUT", "admin/server-config?subsystem=scanner", map[string]any{"revision": conf.Revision, "values": map[string]string{"speed": "fast"}}, 409)
	json.Unmarshal(c.call("GET", "admin/server-config?subsystem=scanner", nil, 200), &conf)
	if conf.Fields[0].Value != "slow" || len(conf.History) == 0 {
		t.Fatalf("%+v", conf)
	}
	c.call("PUT", "admin/server-config?subsystem=scanner", map[string]any{"revision": conf.Revision, "values": map[string]string{"speed": original}}, 200)
	var metric serversvc.LiveMetrics
	json.Unmarshal(c.call("GET", "admin/server-metrics", nil, 200), &metric)
	if metric.Input == nil || metric.Output == nil {
		t.Fatal("no traffic metrics")
	}
	var pools presentation.PoolsView
	json.Unmarshal(c.call("GET", "admin/server-pools", nil, 200), &pools)
	if pools.Supported {
		t.Fatal("single drive must be unsupported")
	}
	// A separate, disposable TWO-POOL deployment. No production endpoints.
	jar, _ := cookiejar.New(nil)
	p := &apiClient{T: t, Http: &http.Client{Jar: jar, Timeout: 30 * time.Second}, Origin: srv.URL}
	status, reply := p.request("POST", "/api/sessions", runtimesvc.Connection{Endpoint: "http://127.0.0.1:19002", AccessKey: "minio-web-test", SecretKey: "minio-web-test-secret", Region: "us-east-1"})
	if status != 201 {
		t.Fatal(status, string(reply))
	}
	var login struct{ ID, CSRF string }
	json.Unmarshal(reply, &login)
	p.Base = "/api/sessions/" + login.ID
	p.CSRF = login.CSRF
	json.Unmarshal(p.call("GET", "admin/server-pools", nil, 200), &pools)
	if !pools.RebalanceKnown || len(pools.Pools) != 2 {
		t.Fatalf("%+v", pools)
	}
	if pools.Rebalancing {
		p.call("POST", "admin/server-pools", map[string]any{"operation": "stop", "revision": pools.Revision, "confirmation": "ОСТАНОВИТЬ"}, 200)
		json.Unmarshal(p.call("GET", "admin/server-pools", nil, 200), &pools)
	}
	p.call("POST", "admin/server-pools", map[string]any{"operation": "start", "revision": pools.Revision, "confirmation": "ПЕРЕРАСПРЕДЕЛИТЬ"}, 200)
	json.Unmarshal(p.call("GET", "admin/server-pools", nil, 200), &pools)
	if pools.Rebalancing {
		p.call("POST", "admin/server-pools", map[string]any{"operation": "stop", "revision": pools.Revision, "confirmation": "ОСТАНОВИТЬ"}, 200)
	}
	// Generate real S3 calls so lazy operation counters appear.
	client, _ := minio.New("127.0.0.1:19000", &minio.Options{Creds: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", "")})
	bucket := "admin-metrics-" + strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:8]))
	ctx := context.Background()
	if e := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); e != nil {
		t.Fatal(e)
	}
	defer client.RemoveBucket(ctx, bucket)
	if _, e := client.PutObject(ctx, bucket, "file", strings.NewReader("data"), 4, minio.PutObjectOptions{}); e != nil {
		t.Fatal(e)
	}
	obj, e := client.GetObject(ctx, bucket, "file", minio.GetObjectOptions{})
	if e != nil {
		t.Fatal(e)
	}
	io.Copy(io.Discard, obj)
	obj.Close()
	client.RemoveObject(ctx, bucket, "file", minio.RemoveObjectOptions{})
	deadline := time.Now().Add(12 * time.Second)
	for {
		json.Unmarshal(c.call("GET", "admin/server-metrics", nil, 200), &metric)
		found := false
		for _, a := range metric.APIs {
			if strings.EqualFold(a.API, "GetObject") && a.Requests != nil {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no native GetObject counter: %+v", metric)
		}
		time.Sleep(time.Second)
	}
	fmt.Println("native configuration/history, S3 metrics and two-pool rebalance verified")
}

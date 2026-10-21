package integration

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"

	apihttp "minio-web/api/src/http/api"
	"minio-web/api/src/http/presentation"
	runtimesvc "minio-web/api/src/services/runtime"
	"minio-web/api/src/services/securetoken"
	serversvc "minio-web/api/src/services/server"
)

func TestServerDiagnosticProjection(t *testing.T) {
	info := madmin.InfoMessage{}
	info.Backend.TotalSets = []int{2, 3}
	info.Backend.DrivesPerSet = []int{4}
	info.Servers = []madmin.ServerProperties{{Endpoint: "node2", PoolNumbers: []int{1, 2}, NumCPU: 8, GoMaxProcs: 4, RuntimeVersion: "go1.24", CommitID: "commit", Edition: "community", MinioEnvVars: map[string]string{"SECRET": "sensitive-value"}, Network: map[string]string{"node3": "upstream-sensitive-value", "node1": "connection attempt timedout"}, GCStats: &madmin.GCStats{NumGC: 4, PauseTotal: 2500 * time.Microsecond}, Disks: []madmin.Disk{{DrivePath: "/data", State: "ok", TotalSpace: 1000, UsedSpace: 400, AvailableSpace: 600, Major: 8, Minor: 1, PoolIndex: 0, SetIndex: 1, DiskIndex: 2, Scanning: true, Metrics: &madmin.DiskMetrics{TotalWaiting: 2, TotalErrorsAvailability: 3, TotalErrorsTimeout: 1, APICalls: map[string]uint64{"unwanted-api": 10}}}}}, {Endpoint: "node1", Disks: []madmin.Disk{{PoolIndex: -1, SetIndex: -1, DiskIndex: -1}}}}
	result := serversvc.ServerRows(info)
	if result.Nodes[0].Endpoint != "node1" || result.Nodes[1].CPU != 8 || result.Nodes[1].GC.PauseMS != 2.5 || result.Nodes[1].Network[0].State != "timeout" || result.Nodes[1].Network[1].State != "unknown" {
		t.Fatal(result.Nodes)
	}
	if result.Pools[0].Number != 1 || result.Pools[0].Sets != 2 || result.Pools[0].DrivesPerSet != 4 || result.Pools[1].DrivesPerSet != 0 {
		t.Fatal(result.Pools)
	}
	disk := result.Drives[1]
	if disk.Pool != 1 || disk.Set != 2 || disk.Disk != 3 || disk.Metrics.Waiting != 2 || disk.Metrics.Errors != 3 || !disk.Scanning || disk.Major != 8 {
		t.Fatal(disk)
	}
	if result.Drives[0].Pool != 0 || result.Drives[0].Metrics != nil || result.Nodes[0].GC != nil || result.Usage != nil {
		t.Fatal("missing data became known")
	}
	raw, _ := json.Marshal(result)
	for _, secret := range []string{"sensitive-value", "unwanted-api", "minio_env_vars", "license"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("unwanted SDK field exposed", secret)
		}
	}
}

func TestServerDiagnosticsIntegration(t *testing.T) {
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
	var snapshot presentation.ServerSnapshot
	if err := json.Unmarshal(root.call("GET", "admin/server-info", nil, 200), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Nodes) != 1 || len(snapshot.Drives) != 1 {
		t.Fatalf("unexpected fixture topology %+v", snapshot)
	}
	node := snapshot.Nodes[0]
	if node.CPU < 1 || node.GoMaxProcs < 1 || node.Runtime == "" || node.Version == "" || node.Memory == 0 {
		t.Fatalf("missing actual runtime data %+v", node)
	}
	disk := snapshot.Drives[0]
	if disk.Total == 0 || disk.Used > disk.Total || disk.Available > disk.Total || disk.Path != "/data" || disk.Metrics == nil {
		t.Fatalf("missing actual drive data %+v", disk)
	}
	if snapshot.Usage == nil && snapshot.UsageError == "" {
		t.Fatal("missing explicit scanner state")
	}
	// A ServerInfo operator can see diagnostics even without DataUsageInfo permission.
	admin, _ := madmin.New("127.0.0.1:19000", "minio-web-test", "minio-web-test-secret", false)
	ctx := context.Background()
	name := "server-test-" + strings.ToLower(securetoken.New()[:8])
	defer func() {
		_, _ = admin.DetachPolicy(ctx, madmin.PolicyAssociationReq{User: name, Policies: []string{name}})
		_ = admin.RemoveUser(ctx, name)
		_ = admin.RemoveCannedPolicy(ctx, name)
	}()
	root.call("POST", "admin/users", map[string]any{"acknowledgeNonAtomicCreate": true, "name": name, "secretKey": "Server-fixture-secret-123"}, 200)
	root.call("POST", "admin/policies", map[string]any{"acknowledgeNonAtomicCreate": true, "name": name, "document": json.RawMessage(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:ListAllMyBuckets"],"Resource":["arn:aws:s3:::*"]},{"Effect":"Allow","Action":["admin:ServerInfo"],"Resource":["*"]}]}`)}, 200)
	root.call("POST", "admin/bindings", map[string]any{"user": name, "policy": name}, 200)
	limited := loginTest(t, srv.URL, name, "Server-fixture-secret-123")
	if err := json.Unmarshal(limited.call("GET", "admin/server-info", nil, 200), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Nodes) != 1 || snapshot.Usage != nil || snapshot.UsageError == "" {
		t.Fatalf("optional scanner denial hid nodes or invented usage %+v", snapshot)
	}
}

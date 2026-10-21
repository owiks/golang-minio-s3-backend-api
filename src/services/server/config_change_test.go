package server_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/minio/madmin-go/v3"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/iam"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/server"
)

// No runtime/session dependency: the command must enforce its own boundary.
type configClient struct {
	t          *testing.T
	ctx        context.Context
	policy     *iam.Policy
	issue      *iam.ActionCheckIssue
	raw        string
	readErr    error
	history    []madmin.ConfigHistoryEntry
	historyErr error
	writeErr   error
	restart    bool
	calls      []string
	line       string
	hook       func(string)
}

func (f *configClient) record(ctx context.Context, call string) {
	f.t.Helper()
	if ctx != f.ctx {
		f.t.Fatal("command context replaced")
	}
	f.calls = append(f.calls, call)
	if f.hook != nil {
		f.hook(call)
	}
}
func (f *configClient) RefreshActionPolicy(ctx context.Context) *iam.ActionCheckIssue {
	f.record(ctx, "permission")
	return f.issue
}
func (f *configClient) ActionPolicySnapshot() *iam.Policy { return f.policy }
func (f *configClient) ActorName() string                 { return "fixture-user" }
func (f *configClient) GetConfigKVWithOptions(ctx context.Context, sub string, opts madmin.KVOptions) ([]byte, error) {
	f.record(ctx, "read:"+sub)
	if !opts.Env {
		f.t.Fatal("ENV overrides must be requested")
	}
	return []byte(f.raw), f.readErr
}
func (f *configClient) ListConfigHistoryKV(ctx context.Context, count int) ([]madmin.ConfigHistoryEntry, error) {
	f.record(ctx, "history")
	if count != 100 {
		f.t.Fatal("unbounded history")
	}
	return f.history, f.historyErr
}
func (f *configClient) SetConfigKV(ctx context.Context, line string) (bool, error) {
	f.record(ctx, "write")
	f.line = line
	return f.restart, f.writeErr
}
func newConfigClient(t *testing.T) *configClient {
	t.Helper()
	p, err := iam.Parse([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:ConfigUpdate"],"Resource":["*"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	return &configClient{t: t, ctx: context.Background(), policy: iam.Intersect(p), raw: "api requests_max=0 cluster_deadline=10s replication_priority=auto\n"}
}
func configRevision(t *testing.T, raw, sub string) string {
	t.Helper()
	client := newConfigClient(t)
	client.raw = raw
	view, err := server.ReadServerConfig(client.ctx, client, sub)
	if err != nil {
		t.Fatal(err)
	}
	return view.Revision
}
func assertConfigError(t *testing.T, err error, status int, code string) {
	t.Helper()
	if err == nil || apperror.Public(err).Status != status || apperror.Public(err).Code != code {
		t.Fatalf("want %d/%s, got %v", status, code, err)
	}
}

func TestSaveConfigOwnsAuthorizationAndFreshSnapshot(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(fmt.Sprint(restart), func(t *testing.T) {
			f := newConfigClient(t)
			f.restart = restart
			body := server.ConfigChangeRequest{Revision: configRevision(t, f.raw, "api"), Values: map[string]string{"requests_max": "20", "cluster_deadline": "1m"}}
			// Even an adapter callback cannot change the already accepted command.
			f.hook = func(call string) {
				if call == "permission" {
					body.Values["requests_max"] = "100"
				}
			}
			result, err := server.SaveConfig(f.ctx, f, "api", body)
			if err != nil || !result.OK || result.RestartRequired != restart || f.line != `api cluster_deadline="1m" requests_max="20"` {
				t.Fatalf("result=%+v line=%q err=%v", result, f.line, err)
			}
			if !reflect.DeepEqual(f.calls, []string{"permission", "read:api", "write"}) {
				t.Fatal("unexpected orchestration or repeated write", f.calls)
			}
		})
	}
	for _, sub := range []string{"", "outside_allowlist", "api requests_max=1", "api\nscanner"} {
		t.Run("invalid subsystem "+sub, func(t *testing.T) {
			f := newConfigClient(t)
			_, err := server.SaveConfig(f.ctx, f, sub, server.ConfigChangeRequest{Revision: "caller", Values: map[string]string{"injected": "1"}})
			assertConfigError(t, err, 400, "InvalidSubsystem")
			if len(f.calls) != 0 {
				t.Fatal("invalid subsystem reached adapter", f.calls)
			}
			_, err = server.ReadServerConfig(f.ctx, f, sub)
			assertConfigError(t, err, 400, "InvalidSubsystem")
			_, err = server.LoadConfig(f.ctx, f, sub)
			assertConfigError(t, err, 400, "InvalidSubsystem")
			if len(f.calls) != 0 {
				t.Fatal("invalid read reached adapter", f.calls)
			}
		})
	}
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprint("permission unknown=", unknown), func(t *testing.T) {
			f := newConfigClient(t)
			code := "AccessDenied"
			if unknown {
				f.issue = &iam.ActionCheckIssue{}
				code = "PermissionUnknown"
			} else {
				f.policy = nil
			}
			_, err := server.SaveConfig(f.ctx, f, "api", server.ConfigChangeRequest{Revision: "caller", Values: map[string]string{"requests_max": "20"}})
			assertConfigError(t, err, 403, code)
			if !reflect.DeepEqual(f.calls, []string{"permission"}) {
				t.Fatal("read/write performed before authorization", f.calls)
			}
		})
	}
}

func TestSaveConfigRejectsUnsafeAndStaleChanges(t *testing.T) {
	cases := []struct {
		name, raw, rev, key, value, code string
		status                           int
	}{
		{name: "stale revision", rev: "caller", key: "requests_max", value: "1", status: 409, code: "ServerConfigChanged"},
		{name: "empty revision", rev: "empty", key: "requests_max", value: "1", status: 409, code: "ServerConfigChanged"},
		{name: "fresh ENV override", raw: "# MINIO_API_REQUESTS_MAX=100\napi requests_max=0\n", key: "requests_max", value: "1", status: 409, code: "ConfigEnvironmentOverride"},
		{name: "unknown key", key: "root_access", value: "off", status: 400, code: "InvalidConfigKey"},
		{name: "absent field", raw: "api cluster_deadline=10s", key: "requests_max", value: "1", status: 400, code: "InvalidConfigKey"},
		{name: "no editable fields", raw: "api root_access=on", key: "requests_max", value: "1", status: 400, code: "InvalidConfigKey"},
		{name: "command injection", key: "requests_max", value: "1\nscanner speed=fast", status: 400, code: "InvalidConfigValue"},
		{name: "negative number", key: "requests_max", value: "-1", status: 400, code: "InvalidConfigValue"},
		{name: "number limit", key: "requests_max", value: "1000001", status: 400, code: "InvalidConfigValue"},
		{name: "invalid duration", key: "cluster_deadline", value: "never", status: 400, code: "InvalidConfigValue"},
		{name: "duration limit", key: "cluster_deadline", value: "9000h", status: 400, code: "InvalidConfigValue"},
		{name: "invalid option", key: "replication_priority", value: "injected", status: 400, code: "InvalidConfigValue"},
		{name: "empty value", key: "requests_max", value: "", status: 400, code: "InvalidConfigValue"},
		{name: "empty values", status: 400, code: "InvalidConfigValues"},
		{name: "too many values", status: 400, code: "InvalidConfigValues"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newConfigClient(t)
			if tc.raw != "" {
				f.raw = tc.raw
			}
			body := server.ConfigChangeRequest{Revision: configRevision(t, f.raw, "api"), Values: map[string]string{}}
			if tc.rev != "" {
				body.Revision = tc.rev
				if tc.rev == "empty" {
					body.Revision = ""
				}
			}
			if tc.key != "" {
				body.Values[tc.key] = tc.value
			}
			if tc.name == "too many values" {
				for i := range 31 {
					body.Values[fmt.Sprint(i)] = "1"
				}
			}
			result, err := server.SaveConfig(f.ctx, f, "api", body)
			assertConfigError(t, err, tc.status, tc.code)
			if result.OK || !reflect.DeepEqual(f.calls, []string{"permission", "read:api"}) {
				t.Fatal("unsafe write or history request", result, f.calls)
			}
		})
	}
	t.Run("snapshot changes after caller read", func(t *testing.T) {
		f := newConfigClient(t)
		body := server.ConfigChangeRequest{Revision: configRevision(t, f.raw, "api"), Values: map[string]string{"requests_max": "20"}}
		f.raw = "api requests_max=100 cluster_deadline=10s replication_priority=auto"
		_, err := server.SaveConfig(f.ctx, f, "api", body)
		assertConfigError(t, err, 409, "ServerConfigChanged")
		if f.line != "" {
			t.Fatal("caller snapshot trusted")
		}
	})
	for _, tc := range []struct{ sub, raw, key, value, line string }{
		{"scanner", "scanner speed=default", "speed", "fast", `scanner speed="fast"`},
		{"heal", "heal drive_workers=1", "drive_workers", "", `heal drive_workers=""`},
	} {
		t.Run(tc.sub, func(t *testing.T) {
			f := newConfigClient(t)
			f.raw = tc.raw
			_, err := server.SaveConfig(f.ctx, f, tc.sub, server.ConfigChangeRequest{Revision: configRevision(t, f.raw, tc.sub), Values: map[string]string{tc.key: tc.value}})
			if err != nil || f.line != tc.line {
				t.Fatalf("line=%q err=%v", f.line, err)
			}
		})
	}
}

func TestSaveConfigRestoresOnlySafeHistoryAgainstCurrentState(t *testing.T) {
	for _, tc := range []struct {
		name, data, raw, id, revision, code string
		values                              map[string]string
		status                              int
	}{
		{name: "restore", data: `api requests_max="50" cluster_deadline="1m"`},
		{name: "wrong subsystem", data: "scanner speed=fast", status: 400, code: "InvalidConfigValues"},
		{name: "mixed secrets", data: "api requests_max=1 root_access=off", status: 400, code: "InvalidConfigValues"},
		{name: "multiple subsystems", data: "api requests_max=1\nscanner speed=fast", status: 400, code: "InvalidConfigValues"},
		{name: "invalid value", data: "api requests_max=-1", status: 400, code: "InvalidConfigValues"},
		{name: "missing history", id: "missing", data: "api requests_max=1", status: 400, code: "InvalidConfigValues"},
		{name: "invalid ID", id: "bad\nID", status: 400, code: "InvalidConfigHistory"},
		{name: "values and restore", data: "api requests_max=1", values: map[string]string{"requests_max": "2"}, status: 400, code: "InvalidConfigHistory"},
		{name: "stale restore", data: "api requests_max=1", revision: "old", status: 409, code: "ServerConfigChanged"},
		{name: "ENV override", data: "api requests_max=1", raw: "# MINIO_API_REQUESTS_MAX=100\napi requests_max=0", status: 409, code: "ConfigEnvironmentOverride"},
		{name: "field disappeared", data: "api requests_max=1", raw: "api cluster_deadline=10s", status: 400, code: "InvalidConfigKey"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newConfigClient(t)
			f.history = []madmin.ConfigHistoryEntry{{RestoreID: "entry", Data: tc.data}}
			if tc.raw != "" {
				f.raw = tc.raw
			}
			body := server.ConfigChangeRequest{Revision: configRevision(t, f.raw, "api"), RestoreID: "entry", Values: tc.values}
			if tc.id != "" {
				body.RestoreID = tc.id
			}
			if tc.revision != "" {
				body.Revision = tc.revision
			}
			result, err := server.SaveConfig(f.ctx, f, "api", body)
			if tc.status != 0 {
				assertConfigError(t, err, tc.status, tc.code)
				if result.OK || f.line != "" {
					t.Fatal("unsafe restore written")
				}
			} else if err != nil || !result.OK || f.line != `api cluster_deadline="1m" requests_max="50"` || !reflect.DeepEqual(f.calls, []string{"permission", "read:api", "history", "write"}) {
				t.Fatalf("restore=%+v calls=%v line=%q err=%v", result, f.calls, f.line, err)
			}
		})
	}
}

func TestConfigFailuresNeverRetryWrites(t *testing.T) {
	for _, phase := range []string{"read", "oversize", "history", "write"} {
		t.Run(phase, func(t *testing.T) {
			f := newConfigClient(t)
			body := server.ConfigChangeRequest{Revision: configRevision(t, f.raw, "api"), Values: map[string]string{"requests_max": "20"}}
			failure := errors.New("upstream failed")
			code := "ServerOperationFailed"
			wantCalls := []string{"permission", "read:api"}
			switch phase {
			case "read":
				f.readErr = failure
			case "oversize":
				f.raw = strings.Repeat("x", 256*1024+1)
				code = "ConfigTooLarge"
			case "history":
				f.historyErr = failure
				body.Values, body.RestoreID = nil, "entry"
				wantCalls = append(wantCalls, "history")
			case "write":
				f.writeErr = failure
				wantCalls = append(wantCalls, "write")
			}
			result, err := server.SaveConfig(f.ctx, f, "api", body)
			assertConfigError(t, err, 502, code)
			if result.OK || !reflect.DeepEqual(f.calls, wantCalls) {
				t.Fatal("failure continued or retried", result, f.calls)
			}
		})
	}
	for _, phase := range []string{"before", "read:api", "history"} {
		t.Run("cancel "+phase, func(t *testing.T) {
			f := newConfigClient(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.ctx = ctx
			body := server.ConfigChangeRequest{Revision: configRevision(t, f.raw, "api"), Values: map[string]string{"requests_max": "20"}}
			if phase == "history" {
				body.Values, body.RestoreID = nil, "entry"
				f.history = []madmin.ConfigHistoryEntry{{RestoreID: "entry", Data: "api requests_max=20"}}
			}
			if phase == "before" {
				cancel()
			} else {
				f.hook = func(call string) {
					if call == phase {
						cancel()
					}
				}
			}
			_, err := server.SaveConfig(ctx, f, "api", body)
			if !errors.Is(err, context.Canceled) || f.line != "" || (phase == "before" && len(f.calls) != 0) {
				t.Fatalf("canceled command continued: calls=%v err=%v", f.calls, err)
			}
		})
	}
}

func TestConfigReadResultsAreIndependent(t *testing.T) {
	const raw = "api replication_priority=auto requests_max=0"
	baseline := configRevision(t, raw, "api")
	// Each goroutine mutates all returned containers while other reads and
	// validators run. A shared catalog or nested option slice causes a race.
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 20 {
				fields, err := server.ConfigFields(raw, "api")
				if err != nil || len(fields) != 2 || fields[1].Options[0] != "auto" || !server.ConfigValueValid(fields[1], "auto") || server.ConfigValueValid(fields[1], "injected") {
					t.Error("catalog changed", fields, err)
					return
				}
				fields[1].Options[0] = "injected"
				fields[0].Key = "root_access"
				fields = append(fields, server.ConfigField{Key: "injected"})
				if len(fields) != 3 || fields[2].Key != "injected" {
					t.Error("returned fields could not be independently extended", fields)
					return
				}
				history := server.ConfigHistory([]madmin.ConfigHistoryEntry{{RestoreID: "entry", Data: raw}}, "api")
				if len(history) != 1 || history[0].Values["replication_priority"] != "auto" {
					t.Error("history changed", history)
					return
				}
				history[0].Values["replication_priority"] = "injected"
			}
		})
	}
	wg.Wait()
	if configRevision(t, raw, "api") != baseline {
		t.Fatal("output mutation changed revision")
	}
	f := newConfigClient(t)
	f.raw = raw
	_, err := server.SaveConfig(f.ctx, f, "api", server.ConfigChangeRequest{Revision: baseline, Values: map[string]string{"replication_priority": "injected"}})
	assertConfigError(t, err, 400, "InvalidConfigValue")
	for _, sub := range []string{"scanner", "heal"} {
		raw := "scanner speed=default"
		if sub == "heal" {
			raw = "heal bitrotscan=off"
		}
		first, _ := server.ConfigFields(raw, sub)
		first[0].Options[0] = "injected"
		second, _ := server.ConfigFields(raw, sub)
		if second[0].Options[0] == "injected" {
			t.Fatal("shared options", sub)
		}
	}
}

func TestLoadConfigHistoryFailureIsOptional(t *testing.T) {
	f := newConfigClient(t)
	f.policy = nil // GET depends only on the reader port, not ConfigUpdate.
	f.historyErr = errors.New("history denied")
	view, err := server.LoadConfig(f.ctx, f, "api")
	if err != nil || view.Revision == "" || len(view.Fields) != 3 || messages.Empty(view.HistoryError) || view.History == nil || !reflect.DeepEqual(f.calls, []string{"read:api", "history"}) {
		t.Fatalf("optional history broke read: %+v calls=%v err=%v", view, f.calls, err)
	}
}

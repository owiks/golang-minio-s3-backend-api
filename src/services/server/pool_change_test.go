package server_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/minio/madmin-go/v3"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/iam"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/server"
)

type poolChangeClient struct {
	policy *iam.Policy
	err    error
	calls  []string
}

func (f *poolChangeClient) ActionPolicySnapshot() *iam.Policy { return f.policy }
func (f *poolChangeClient) ActorName() string                 { return "fixture-user" }
func (f *poolChangeClient) RefreshActionPolicy(context.Context) *iam.ActionCheckIssue {
	f.calls = append(f.calls, "permission")
	return nil
}
func (f *poolChangeClient) ListPoolsStatus(context.Context) ([]madmin.PoolStatus, error) {
	f.calls = append(f.calls, "list")
	return []madmin.PoolStatus{{ID: 0, CmdLine: "pool-A"}, {ID: 1, CmdLine: "pool-B"}}, nil
}
func (f *poolChangeClient) RebalanceStatus(context.Context) (madmin.RebalanceStatus, error) {
	f.calls = append(f.calls, "rebalance-status")
	return madmin.RebalanceStatus{}, nil
}
func (f *poolChangeClient) ServerInfo(context.Context, ...func(*madmin.ServerInfoOpts)) (madmin.InfoMessage, error) {
	f.calls = append(f.calls, "health")
	return madmin.InfoMessage{Servers: []madmin.ServerProperties{{State: "online", Disks: []madmin.Disk{
		{State: "ok", PoolIndex: 0},
		{State: "ok", PoolIndex: 1},
	}}}}, nil
}
func (f *poolChangeClient) RebalanceStart(context.Context) (string, error) {
	f.calls = append(f.calls, "start")
	return "", f.err
}
func (f *poolChangeClient) DecommissionPool(_ context.Context, pool string) error {
	f.calls = append(f.calls, "decommission:"+pool)
	return f.err
}
func (f *poolChangeClient) RebalanceStop(context.Context) error {
	f.calls = append(f.calls, "unexpected-stop")
	return f.err
}
func (f *poolChangeClient) CancelDecommissionPool(context.Context, string) error {
	f.calls = append(f.calls, "unexpected-cancel")
	return f.err
}

func TestChangePoolsReturnsCommandFailureWithoutRepeatingMutation(t *testing.T) {
	for _, operation := range []string{"start", "decommission"} {
		for _, tc := range []struct {
			name   string
			err    error
			status int
			code   string
		}{
			{name: "success"},
			{name: "access-denied", err: madmin.ErrorResponse{Code: "AccessDenied"}, status: 403, code: "AccessDenied"},
			{name: "unconfirmed-result", err: errors.New("connection closed after command dispatch"), status: 502, code: "ServerOperationFailed"},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				policy, err := iam.Parse([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["admin:Rebalance","admin:Decommission"],"Resource":["*"]}]}`))
				if err != nil {
					t.Fatal(err)
				}
				client := &poolChangeClient{policy: iam.Intersect(policy), err: tc.err}
				ctx := context.Background()
				view, _, err := server.ReadPools(ctx, client)
				if err != nil {
					t.Fatal(err)
				}
				client.calls = nil
				var command server.PoolCommand = server.StartRebalanceCommand{Revision: view.Revision, Confirmation: "ПЕРЕРАСПРЕДЕЛИТЬ"}
				mutation := "start"
				if operation == "decommission" {
					command = server.DecommissionPoolCommand{Pool: 2, Revision: view.Revision, Confirmation: "ПУЛ 2"}
					mutation = "decommission:pool-B"
				}
				err = server.ChangePools(ctx, client, command)
				if tc.err == nil {
					if err != nil {
						t.Fatal("successful command rejected:", err)
					}
				} else if err == nil || apperror.Public(err).Status != tc.status || apperror.Public(err).Code != tc.code {
					t.Fatalf("want %d/%s, got %v", tc.status, tc.code, err)
				}
				want := []string{"permission", "list", "rebalance-status", "health", mutation}
				if !reflect.DeepEqual(client.calls, want) {
					t.Fatalf("unexpected orchestration or repeated mutation: got %v, want %v", client.calls, want)
				}
			})
		}
	}
}

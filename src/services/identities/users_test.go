package identities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/minio/madmin-go/v3"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
)

type userStub struct {
	info    madmin.UserInfo
	readErr error
	writes  int
}

func (f *userStub) ListUsers(context.Context) (map[string]madmin.UserInfo, error) {
	return map[string]madmin.UserInfo{"alice": f.info}, nil
}
func (f *userStub) GetUserInfo(context.Context, string) (madmin.UserInfo, error) {
	return f.info, f.readErr
}
func (f *userStub) AddUser(context.Context, string, string) error { f.writes++; return nil }
func (f *userStub) SetUserStatus(context.Context, string, madmin.AccountStatus) error {
	f.writes++
	return nil
}
func (f *userStub) RemoveUser(context.Context, string) error { f.writes++; return nil }
func TestUserUseCasesRejectObservedExistingAndProtectCurrentIdentity(t *testing.T) {
	ctx := context.Background()
	f := &userStub{}
	if err := CreateUser(ctx, f, CreateUserRequest{Name: "alice", SecretKey: "valid-secret"}); apperror.Public(err).Code != "AlreadyExists" {
		t.Fatal(err)
	}
	f.readErr = errors.New("PRIVATE-UPSTREAM-ERROR")
	if err := CreateUser(ctx, f, CreateUserRequest{Name: "alice", SecretKey: "valid-secret"}); err != f.readErr {
		t.Fatal("upstream error ignored")
	}
	disabled := false
	if err := SetUserStatus(ctx, f, "alice", "alice", StatusRequest{Enabled: &disabled}); apperror.Public(err).Code != "CurrentUser" {
		t.Fatal(err)
	}
	if err := DeleteUser(ctx, f, "alice", "alice"); apperror.Public(err).Code != "CurrentUser" {
		t.Fatal(err)
	}
	if f.writes != 0 {
		t.Fatal("guarded operation reached upstream")
	}
	f.readErr = fmt.Errorf("wrapped: %w", madmin.ErrorResponse{Code: "XMinioAdminNoSuchUser"})
	if err := CreateUser(ctx, f, CreateUserRequest{Name: "alice", SecretKey: "valid-secret"}); apperror.Public(err).Code != "UserCreateAcknowledgementRequired" || f.writes != 0 {
		t.Fatal("unacknowledged upsert reached upstream", err)
	}
	if err := CreateUser(ctx, f, CreateUserRequest{Name: "alice", SecretKey: "valid-secret", AcknowledgeNonAtomicCreate: true}); err != nil {
		t.Fatal(err)
	}
	if f.writes != 1 {
		t.Fatal("valid creation not performed")
	}
}
func TestUserResponseExcludesUpstreamCredentials(t *testing.T) {
	f := &userStub{info: madmin.UserInfo{SecretKey: "PRIVATE-SECRET", PolicyName: "read,write", Status: madmin.AccountEnabled}}
	rows, err := ListUsers(context.Background(), f, "alice")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), "secretKey") {
		t.Fatal("credential leaked")
	}
	if len(rows) != 1 || !rows[0].Current || rows[0].Groups == nil || len(rows[0].Policies) != 2 {
		t.Fatal(rows)
	}
}

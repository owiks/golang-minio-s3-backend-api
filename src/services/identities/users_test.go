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

var _ UserAdmin = (*MockUserAdmin)(nil)

func TestUserUseCasesRejectObservedExistingAndProtectCurrentIdentity(t *testing.T) {
	ctx := context.Background()
	missing := fmt.Errorf("wrapped: %w", madmin.ErrorResponse{Code: "XMinioAdminNoSuchUser"})
	for _, acknowledge := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing acknowledged=%v", acknowledge), func(t *testing.T) {
			admin := NewMockUserAdmin(t)
			admin.EXPECT().GetUserInfo(ctx, "alice").Return(madmin.UserInfo{}, nil).Once()
			err := CreateUser(ctx, admin, CreateUserRequest{Name: "alice", SecretKey: "valid-secret", AcknowledgeNonAtomicCreate: acknowledge})
			if apperror.Public(err).Code != "AlreadyExists" {
				t.Fatal(err)
			}
			// Any write fails as an unexpected mock call, even with acknowledgement.
		})
	}
	t.Run("read failure", func(t *testing.T) {
		admin := NewMockUserAdmin(t)
		failure := errors.New("PRIVATE-UPSTREAM-ERROR")
		admin.EXPECT().GetUserInfo(ctx, "alice").Return(madmin.UserInfo{}, failure).Once()
		if err := CreateUser(ctx, admin, CreateUserRequest{Name: "alice", SecretKey: "valid-secret"}); err != failure {
			t.Fatal("upstream error ignored", err)
		}
	})
	t.Run("current user", func(t *testing.T) {
		admin := NewMockUserAdmin(t)
		if err := SetUserStatus(ctx, admin, "alice", "alice", StatusRequest{Enabled: new(false)}); apperror.Public(err).Code != "CurrentUser" {
			t.Fatal(err)
		}
		if err := DeleteUser(ctx, admin, "alice", "alice"); apperror.Public(err).Code != "CurrentUser" {
			t.Fatal(err)
		}
	})
	t.Run("acknowledgement required", func(t *testing.T) {
		admin := NewMockUserAdmin(t)
		admin.EXPECT().GetUserInfo(ctx, "alice").Return(madmin.UserInfo{}, missing).Once()
		if err := CreateUser(ctx, admin, CreateUserRequest{Name: "alice", SecretKey: "valid-secret"}); apperror.Public(err).Code != "UserCreateAcknowledgementRequired" {
			t.Fatal("unacknowledged upsert reached upstream", err)
		}
	})
	for _, failure := range []error{nil, errors.New("uncertain write result")} {
		t.Run(fmt.Sprintf("acknowledged create failure=%v", failure != nil), func(t *testing.T) {
			admin := NewMockUserAdmin(t)
			read := admin.EXPECT().GetUserInfo(ctx, "alice").Return(madmin.UserInfo{}, missing).Once()
			admin.EXPECT().AddUser(ctx, "alice", "valid-secret").Return(failure).Once().NotBefore(read)
			if err := CreateUser(ctx, admin, CreateUserRequest{Name: "alice", SecretKey: "valid-secret", AcknowledgeNonAtomicCreate: true}); err != failure {
				t.Fatal("creation result changed", err)
			}
		})
	}
}
func TestUserResponseExcludesUpstreamCredentials(t *testing.T) {
	ctx := context.Background()
	admin := NewMockUserAdmin(t)
	admin.EXPECT().ListUsers(ctx).Return(map[string]madmin.UserInfo{
		"alice": {SecretKey: "PRIVATE-SECRET", PolicyName: "read,write", Status: madmin.AccountEnabled},
	}, nil).Once()
	rows, err := ListUsers(ctx, admin, "alice")
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

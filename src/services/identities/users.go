package identities

import (
	"context"
	"sort"

	"github.com/minio/madmin-go/v3"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/validation"
)

type UserAdmin interface {
	ListUsers(context.Context) (map[string]madmin.UserInfo, error)
	GetUserInfo(context.Context, string) (madmin.UserInfo, error)
	AddUser(context.Context, string, string) error
	SetUserStatus(context.Context, string, madmin.AccountStatus) error
	RemoveUser(context.Context, string) error
}
type CreateUserRequest struct {
	Name      string `json:"name" validate:"required,minBytes=3,maxBytes=128"`
	SecretKey string `json:"secretKey" validate:"required,minBytes=8,maxBytes=256"`
	// Explicit consent to MinIO's upsert semantics after the existence check.
	// This is not a conditional-create guarantee or consent to change a found user.
	AcknowledgeNonAtomicCreate bool `json:"acknowledgeNonAtomicCreate"`
}
type StatusRequest struct {
	Enabled *bool `json:"enabled" validate:"required"`
}
type UserDetails struct {
	Name     string               `json:"name"`
	Status   madmin.AccountStatus `json:"status"`
	Policies []string             `json:"policies"`
	Groups   []string             `json:"groups"`
	Current  bool                 `json:"current"`
}

func userDetails(name, actor string, u madmin.UserInfo) UserDetails {
	groups := u.MemberOf
	if groups == nil {
		groups = []string{}
	}
	return UserDetails{Name: name, Status: u.Status, Policies: SplitPolicies(u.PolicyName), Groups: groups, Current: name == actor}
}
func ListUsers(ctx context.Context, admin UserAdmin, actor string) ([]UserDetails, error) {
	users, err := admin.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(users))
	for name := range users {
		names = append(names, name)
	}
	sort.Strings(names)
	rows := make([]UserDetails, 0, len(names))
	for _, name := range names {
		rows = append(rows, userDetails(name, actor, users[name]))
	}
	return rows, nil
}
func GetUser(ctx context.Context, admin UserAdmin, actor, name string) (UserDetails, error) {
	u, err := admin.GetUserInfo(ctx, name)
	if err != nil {
		return UserDetails{}, err
	}
	return userDetails(name, actor, u), nil
}
func CreateUser(ctx context.Context, admin UserAdmin, b CreateUserRequest) error {
	if !validation.ValidName(b.Name) || len(b.Name) < 3 || len(b.SecretKey) < 8 || len(b.SecretKey) > 256 {
		return apperror.New(400, "InvalidUser", messages.IdentitiesInvalidUser)
	}
	if _, err := admin.GetUserInfo(ctx, b.Name); err == nil {
		return apperror.New(409, "AlreadyExists", messages.IdentitiesAlreadyExistsUsers)
	} else if !Missing(err, "XMinioAdminNoSuchUser") {
		return err
	}
	if !b.AcknowledgeNonAtomicCreate {
		return apperror.New(400, "UserCreateAcknowledgementRequired", messages.IdentitiesUserCreateAcknowledgementRequired)
	}
	return admin.AddUser(ctx, b.Name, b.SecretKey)
}
func SetUserStatus(ctx context.Context, admin UserAdmin, actor, name string, b StatusRequest) error {
	if b.Enabled == nil {
		return apperror.New(400, "InvalidStatus", messages.IdentitiesInvalidStatus)
	}
	if name == actor && !*b.Enabled {
		return apperror.New(409, "CurrentUser", messages.IdentitiesCurrentUser)
	}
	status := madmin.AccountDisabled
	if *b.Enabled {
		status = madmin.AccountEnabled
	}
	return admin.SetUserStatus(ctx, name, status)
}
func DeleteUser(ctx context.Context, admin UserAdmin, actor, name string) error {
	if name == actor {
		return apperror.New(409, "CurrentUser", messages.IdentitiesCurrentUserUsers)
	}
	return admin.RemoveUser(ctx, name)
}

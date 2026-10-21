package identities

import (
	"context"
	"sort"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/validation"
)

type GroupAdmin interface {
	ListGroups(context.Context) ([]string, error)
	GetGroupDescription(context.Context, string) (*madmin.GroupDesc, error)
	UpdateGroupMembers(context.Context, madmin.GroupAddRemove) error
	SetGroupStatus(context.Context, string, madmin.GroupStatus) error
}
type CreateGroupRequest struct {
	AcknowledgeNonAtomicCreate bool     `json:"acknowledgeNonAtomicCreate"`
	Name                       string   `json:"name" validate:"required,maxBytes=128"`
	Members                    []string `json:"members" validate:"required,minBytes=1,maxBytes=100"`
}
type GroupMembersRequest struct {
	Members []string `json:"members" validate:"required,minBytes=1,maxBytes=100"`
	Remove  bool     `json:"remove"`
}
type GroupDetails struct {
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	Members  []string `json:"members"`
	Policies []string `json:"policies"`
}

func ListGroups(ctx context.Context, admin GroupAdmin) ([]string, error) {
	groups, err := admin.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	if groups == nil {
		groups = []string{}
	}
	sort.Strings(groups)
	return groups, nil
}
func GetGroup(ctx context.Context, admin GroupAdmin, name string) (GroupDetails, error) {
	g, err := admin.GetGroupDescription(ctx, name)
	if err != nil {
		return GroupDetails{}, err
	}
	members := g.Members
	if members == nil {
		members = []string{}
	}
	return GroupDetails{Name: g.Name, Status: g.Status, Members: members, Policies: SplitPolicies(g.Policy)}, nil
}
func CreateGroup(ctx context.Context, admin GroupAdmin, b CreateGroupRequest) error {
	if !validation.ValidName(b.Name) || len(b.Members) == 0 {
		return apperror.New(400, "InvalidGroup", messages.IdentitiesInvalidGroup)
	}
	if _, err := admin.GetGroupDescription(ctx, b.Name); err == nil {
		return apperror.New(409, "AlreadyExists", messages.IdentitiesAlreadyExists)
	} else if !Missing(err, "XMinioAdminNoSuchGroup") {
		return err
	}
	if err := validation.ValidateMembers(b.Members); err != nil {
		return err
	}
	if !b.AcknowledgeNonAtomicCreate {
		return apperror.New(400, "GroupCreateAcknowledgementRequired", messages.IdentitiesGroupCreateAcknowledgementRequired)
	}
	return admin.UpdateGroupMembers(ctx, madmin.GroupAddRemove{Group: b.Name, Members: b.Members, Status: madmin.GroupEnabled})
}
func UpdateGroupMembers(ctx context.Context, admin GroupAdmin, name string, b GroupMembersRequest) error {
	if err := validation.ValidateMembers(b.Members); err != nil {
		return err
	}
	if _, err := admin.GetGroupDescription(ctx, name); err != nil {
		return err
	}
	return admin.UpdateGroupMembers(ctx, madmin.GroupAddRemove{Group: name, Members: b.Members, IsRemove: b.Remove})
}
func SetGroupStatus(ctx context.Context, admin GroupAdmin, name string, b StatusRequest) error {
	if b.Enabled == nil {
		return apperror.New(400, "InvalidStatus", messages.IdentitiesInvalidStatus)
	}
	status := madmin.GroupDisabled
	if *b.Enabled {
		status = madmin.GroupEnabled
	}
	return admin.SetGroupStatus(ctx, name, status)
}
func DeleteGroup(ctx context.Context, admin GroupAdmin, name string) error {
	group, err := admin.GetGroupDescription(ctx, name)
	if err != nil {
		return err
	}
	if len(group.Members) > 0 {
		return apperror.New(409, "GroupNotEmpty", messages.IdentitiesGroupNotEmpty)
	}
	return admin.UpdateGroupMembers(ctx, madmin.GroupAddRemove{Group: name, IsRemove: true})
}

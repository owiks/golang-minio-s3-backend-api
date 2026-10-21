package identities

import (
	"context"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/validation"
)

type BindingAdmin interface {
	GetPolicyEntities(context.Context, madmin.PolicyEntitiesQuery) (madmin.PolicyEntitiesResult, error)
	AttachPolicy(context.Context, madmin.PolicyAssociationReq) (madmin.PolicyAssociationResp, error)
	DetachPolicy(context.Context, madmin.PolicyAssociationReq) (madmin.PolicyAssociationResp, error)
}
type BindingRequest struct {
	User   string `json:"user,omitempty"`
	Group  string `json:"group,omitempty"`
	Policy string `json:"policy" validate:"required"`
	Remove bool   `json:"remove"`
}

func GetBindings(ctx context.Context, admin BindingAdmin, user, group, policy string) (Bindings, error) {
	query := madmin.PolicyEntitiesQuery{}
	if user != "" {
		query.Users = []string{user}
	}
	if group != "" {
		query.Groups = []string{group}
	}
	if policy != "" {
		query.Policy = []string{policy}
	}
	if len(query.Users)+len(query.Groups)+len(query.Policy) != 1 {
		return Bindings{}, apperror.New(400, "InvalidTarget", messages.IdentitiesInvalidTarget)
	}
	for _, target := range []string{user, group, policy} {
		if target != "" && !validation.ValidName(target) {
			return Bindings{}, apperror.New(400, "InvalidTarget", messages.IdentitiesInvalidTargetBindings)
		}
	}
	value, err := admin.GetPolicyEntities(ctx, query)
	return bindingResult(value), err
}
func SaveBinding(ctx context.Context, admin BindingAdmin, b BindingRequest) error {
	if !validation.ValidName(b.Policy) || (b.User == "") == (b.Group == "") || b.User != "" && !validation.ValidName(b.User) || b.Group != "" && !validation.ValidName(b.Group) {
		return apperror.New(400, "InvalidBinding", messages.IdentitiesInvalidBinding)
	}
	req := madmin.PolicyAssociationReq{User: b.User, Group: b.Group, Policies: []string{b.Policy}}
	if b.Remove {
		_, err := admin.DetachPolicy(ctx, req)
		return err
	}
	_, err := admin.AttachPolicy(ctx, req)
	return err
}

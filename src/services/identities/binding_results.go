package identities

import (
	"github.com/minio/madmin-go/v3"
	"slices"
	"time"
)

type Bindings struct {
	Timestamp      time.Time            `json:"timestamp"`
	UserMappings   []UserPolicyMapping  `json:"userMappings,omitempty"`
	GroupMappings  []GroupPolicyMapping `json:"groupMappings,omitempty"`
	PolicyMappings []PolicyMapping      `json:"policyMappings,omitempty"`
}
type UserPolicyMapping struct {
	User             string               `json:"user"`
	Policies         []string             `json:"policies"`
	MemberOfMappings []GroupPolicyMapping `json:"memberOfMappings,omitempty"`
}
type GroupPolicyMapping struct {
	Group    string   `json:"group"`
	Policies []string `json:"policies"`
}
type PolicyMapping struct {
	Policy string   `json:"policy"`
	Users  []string `json:"users"`
	Groups []string `json:"groups"`
}

func groupMappings(values []madmin.GroupPolicyEntities) []GroupPolicyMapping {
	if values == nil {
		return nil
	}
	out := make([]GroupPolicyMapping, len(values))
	for i, v := range values {
		out[i] = GroupPolicyMapping{Group: v.Group, Policies: slices.Clone(v.Policies)}
	}
	return out
}
func bindingResult(value madmin.PolicyEntitiesResult) Bindings {
	out := Bindings{Timestamp: value.Timestamp, GroupMappings: groupMappings(value.GroupMappings)}
	if value.UserMappings != nil {
		out.UserMappings = make([]UserPolicyMapping, len(value.UserMappings))
	}
	for i, v := range value.UserMappings {
		out.UserMappings[i] = UserPolicyMapping{User: v.User, Policies: slices.Clone(v.Policies), MemberOfMappings: groupMappings(v.MemberOfMappings)}
	}
	if value.PolicyMappings != nil {
		out.PolicyMappings = make([]PolicyMapping, len(value.PolicyMappings))
	}
	for i, v := range value.PolicyMappings {
		out.PolicyMappings[i] = PolicyMapping{Policy: v.Policy, Users: slices.Clone(v.Users), Groups: slices.Clone(v.Groups)}
	}
	return out
}

package identities

import (
	"context"
	"encoding/json"

	"github.com/minio/madmin-go/v3"
)

// Interfaces belong to the use cases that consume them. Only required
// operations are exposed; connection credentials and session state stay outside.
type GroupSummariesClient interface {
	GetGroupDescription(context.Context, string) (*madmin.GroupDesc, error)
	ListGroups(context.Context) ([]string, error)
}

type PolicySummariesClient interface {
	GetPolicyEntities(context.Context, madmin.PolicyEntitiesQuery) (madmin.PolicyEntitiesResult, error)
	InfoCannedPolicyV2(context.Context, string) (*madmin.PolicyInfo, error)
	ListCannedPolicies(context.Context) (map[string]json.RawMessage, error)
}

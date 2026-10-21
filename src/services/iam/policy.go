// Package iam evaluates conservative UI action hints; MinIO still authorizes every operation.
package iam

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/minio/pkg/v3/policy"
)

// Policy intersects the parent policy with all derived-key restrictions.
// A nil policy or an empty intersection cannot establish an Allow.
type Policy struct{ layers []policy.Policy }

// Intersect owns a detached snapshot of each layer after returning. Callers
// must not mutate inputs during construction, but may freely change them later.
func Intersect(layers ...policy.Policy) *Policy {
	owned := slices.Clone(layers)
	for i, layer := range layers {
		owned[i].Statements = slices.Clone(layer.Statements)
		for j, statement := range layer.Statements {
			// SDK Clone also owns Actions/NotActions, Resources/NotResources
			// and condition functions with their nested values (including IPs).
			owned[i].Statements[j] = statement.Clone()
		}
	}
	return &Policy{layers: owned}
}

type Query struct {
	Action  string `json:"action"`
	Bucket  string `json:"bucket"`
	Key     string `json:"key"`
	Version string `json:"version,omitempty"`
}

func Parse(raw []byte) (policy.Policy, error) {
	p, err := policy.ParseConfig(bytes.NewReader(raw))
	if err != nil {
		return policy.Policy{}, err
	}
	statements := []policy.Statement{}
	for _, statement := range p.Statements {
		encoded, _ := json.Marshal(statement)
		if bytes.Contains(encoded, []byte("${")) {
			// Unknown substitutions may also occur in NotResource. Never guess scope.
			if statement.Effect == policy.Deny {
				return policy.Policy{}, nil
			}
			continue
		}
		if len(statement.Conditions) > 0 {
			if statement.Effect == policy.Allow {
				continue
			}
			statement.Conditions = nil
		}
		statements = append(statements, statement)
	}
	p.Statements = statements
	return *p, nil
}

func (p *Policy) Allows(actor string, q Query) bool {
	if p == nil || len(p.layers) == 0 {
		return false
	}
	if (q.Action == "admin:DeleteUser" || q.Action == "admin:DisableUser" || q.Action == "admin:EnableUser") && q.Key == actor {
		return false
	}
	// MinIO SetUserStatus authorizes both status changes with EnableUser.
	nativeAction := q.Action
	if nativeAction == "admin:DisableUser" {
		nativeAction = "admin:EnableUser"
	}
	args := policy.Args{AccountName: actor, Action: policy.Action(nativeAction), BucketName: q.Bucket, ObjectName: q.Key, ConditionValues: map[string][]string{}}
	for _, layer := range p.layers {
		if !layer.IsAllowed(args) {
			return false
		}
	}
	return true
}

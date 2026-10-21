package identities

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

type GroupSummaryDetails struct {
	Status    string     `json:"status"`
	Members   []string   `json:"members"`
	Policies  []string   `json:"policies"`
	UpdatedAt *time.Time `json:"updatedAt"`
}
type GroupSummary struct {
	Name    string               `json:"name"`
	Details *GroupSummaryDetails `json:"details"`
	Error   messages.Ref         `json:"error,omitempty"`
}
type PolicyDefinition struct {
	Version       string   `json:"version"`
	Statements    int      `json:"statements"`
	Allow         int      `json:"allow"`
	Deny          int      `json:"deny"`
	Actions       []string `json:"actions"`
	NotActions    []string `json:"notActions"`
	Resources     []string `json:"resources"`
	NotResources  []string `json:"notResources"`
	HasConditions bool     `json:"hasConditions"`
}
type PolicyAssignments struct {
	Users  []string `json:"users"`
	Groups []string `json:"groups"`
}
type PolicySummary struct {
	Name             string             `json:"name"`
	Definition       *PolicyDefinition  `json:"definition"`
	DefinitionError  messages.Ref       `json:"definitionError,omitempty"`
	Assignments      *PolicyAssignments `json:"assignments"`
	AssignmentsError messages.Ref       `json:"assignmentsError,omitempty"`
	CreatedAt        *time.Time         `json:"createdAt"`
	UpdatedAt        *time.Time         `json:"updatedAt"`
	DatesError       messages.Ref       `json:"datesError,omitempty"`
}

func KnownTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
func SortedUnique(values []string) []string {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		seen[value] = true
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// Limit fan-out. Failed optional reads must not erase a successfully loaded list.
func ReadSummaryRows(ctx context.Context, n int, read func(int)) error {
	var next atomic.Uint64
	var wait sync.WaitGroup
	for range min(4, n) {
		wait.Go(func() {
			for {
				if ctx.Err() != nil {
					return
				}
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				if ctx.Err() != nil {
					return
				}
				read(i)
			}
		})
	}
	wait.Wait()
	return ctx.Err()
}

func PolicyStrings(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, apperror.New(502, "InvalidPolicyDefinition", messages.BucketsInvalidPolicyDefinition)
	}
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return []string{single}, nil
	}
	var values []string
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return nil, apperror.New(502, "InvalidPolicyDefinition", messages.BucketsInvalidPolicyDefinition)
	}
	return values, nil
}
func SummarizePolicy(raw json.RawMessage) (*PolicyDefinition, error) {
	var doc struct {
		Version   string          `json:"Version"`
		Statement json.RawMessage `json:"Statement"`
	}
	if json.Unmarshal(raw, &doc) != nil || doc.Version == "" {
		return nil, apperror.New(502, "InvalidPolicyDefinition", messages.BucketsInvalidPolicyDefinition)
	}
	statements := bytes.TrimSpace(doc.Statement)
	var rules []json.RawMessage
	if len(statements) > 0 && statements[0] == '{' {
		rules = []json.RawMessage{statements}
	} else if json.Unmarshal(statements, &rules) != nil || rules == nil {
		return nil, apperror.New(502, "InvalidPolicyDefinition", messages.BucketsInvalidPolicyDefinition)
	}
	result := &PolicyDefinition{Version: doc.Version, Statements: len(rules), Actions: []string{}, NotActions: []string{}, Resources: []string{}, NotResources: []string{}}
	for _, raw := range rules {
		var rule struct {
			Effect      string                     `json:"Effect"`
			Action      json.RawMessage            `json:"Action"`
			NotAction   json.RawMessage            `json:"NotAction"`
			Resource    json.RawMessage            `json:"Resource"`
			NotResource json.RawMessage            `json:"NotResource"`
			Condition   map[string]json.RawMessage `json:"Condition"`
		}
		if json.Unmarshal(raw, &rule) != nil {
			return nil, apperror.New(502, "InvalidPolicyDefinition", messages.BucketsInvalidPolicyDefinition)
		}
		switch rule.Effect {
		case "Allow":
			result.Allow++
		case "Deny":
			result.Deny++
		default:
			return nil, apperror.New(502, "InvalidPolicyDefinition", messages.IdentitiesInvalidPolicyDefinition)
		}
		for _, field := range []struct {
			Raw    json.RawMessage `json:"-"`
			Target *[]string       `json:"-"`
		}{{rule.Action, &result.Actions}, {rule.NotAction, &result.NotActions}, {rule.Resource, &result.Resources}, {rule.NotResource, &result.NotResources}} {
			values, err := PolicyStrings(field.Raw)
			if err != nil {
				return nil, err
			}
			*field.Target = append(*field.Target, values...)
		}
		result.HasConditions = result.HasConditions || len(rule.Condition) > 0
	}
	result.Actions = SortedUnique(result.Actions)
	result.NotActions = SortedUnique(result.NotActions)
	result.Resources = SortedUnique(result.Resources)
	result.NotResources = SortedUnique(result.NotResources)
	return result, nil
}

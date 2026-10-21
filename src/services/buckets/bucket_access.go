package buckets

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/minio/pkg/v3/wildcard"

	"minio-web/api/src/services/apperror"
	identitiessvc "minio-web/api/src/services/identities"
	"minio-web/api/src/services/messages"
)

// This is a relationship report, not an effective-policy simulator. Conditions,
// resource exclusions, parent/key intersections and bucket policies are retained
// as separate facts. No report result is used to authorize an operation.
type BucketGrant struct {
	Principals    []messages.Ref `json:"principals,omitempty"`
	NotPrincipals []messages.Ref `json:"notPrincipals,omitempty"`
	Policy        messages.Ref   `json:"policy"`
	Source        messages.Ref   `json:"source"`
	Effect        string         `json:"effect"`
	Actions       []string       `json:"actions"`
	NotActions    []string       `json:"notActions"`
	Resources     []string       `json:"resources"`
	NotResources  []string       `json:"notResources"`
	Conditional   bool           `json:"conditional"`
	Uncertain     bool           `json:"uncertain"`
}
type BucketAccessRow struct {
	Kind             string         `json:"kind"`
	Name             string         `json:"name"`
	Parent           string         `json:"parent,omitempty"`
	Status           string         `json:"status"`
	Grants           []BucketGrant  `json:"grants"`
	RestrictionState string         `json:"restrictionState,omitempty"`
	Restrictions     []BucketGrant  `json:"restrictions,omitempty"`
	Notes            []messages.Ref `json:"notes"`
}
type BucketAccessReport struct {
	Bucket    string            `json:"bucket"`
	CheckedAt time.Time         `json:"checkedAt"`
	Complete  bool              `json:"complete"`
	Warnings  []messages.Ref    `json:"warnings"`
	Rows      []BucketAccessRow `json:"rows"`
}

func GrantPrincipals(raw json.RawMessage) []messages.Ref {
	if len(raw) == 0 {
		return nil
	}
	if values, err := identitiessvc.PolicyStrings(raw); err == nil {
		return messages.Literals(values)
	}
	var kinds map[string]json.RawMessage
	if json.Unmarshal(raw, &kinds) != nil {
		return []messages.Ref{messages.BucketsCouldNotReadPrincipal}
	}
	values := []string{}
	for kind, raw := range kinds {
		items, err := identitiessvc.PolicyStrings(raw)
		if err != nil {
			return []messages.Ref{messages.BucketsCouldNotReadPrincipal}
		}
		for _, item := range items {
			values = append(values, kind+": "+item)
		}
	}
	return messages.Literals(identitiessvc.SortedUnique(values))
}
func ResourceTouchesBucket(resource, bucket string) bool {
	if resource == "*" || strings.Contains(resource, "${") {
		return true
	}
	const prefix = "arn:aws:s3:::"
	if !strings.HasPrefix(resource, prefix) {
		return false
	}
	pattern, _, _ := strings.Cut(strings.TrimPrefix(resource, prefix), "/")
	return wildcard.MatchSimple(pattern, bucket)
}
func BucketGrants(raw json.RawMessage, bucket string) ([]BucketGrant, error) {
	var doc struct{ Statement json.RawMessage }
	if json.Unmarshal(raw, &doc) != nil {
		return nil, apperror.New(502, "InvalidPolicyDefinition", messages.BucketsInvalidPolicyDefinition)
	}
	statements := bytes.TrimSpace(doc.Statement)
	var rules []json.RawMessage
	if len(statements) > 0 && statements[0] == '{' {
		rules = []json.RawMessage{statements}
	} else if json.Unmarshal(statements, &rules) != nil || rules == nil {
		return nil, apperror.New(502, "InvalidPolicyDefinition", messages.BucketsInvalidPolicyDefinition)
	}
	result := []BucketGrant{}
	for _, raw := range rules {
		var rule struct {
			Effect                                   string
			Action, NotAction, Resource, NotResource json.RawMessage
			Condition                                map[string]json.RawMessage
			Principal, NotPrincipal                  json.RawMessage
		}
		if json.Unmarshal(raw, &rule) != nil || (rule.Effect != "Allow" && rule.Effect != "Deny") {
			return nil, apperror.New(502, "InvalidPolicyDefinition", messages.BucketsInvalidPolicyDefinition)
		}
		grant := BucketGrant{Effect: rule.Effect, Conditional: len(rule.Condition) > 0}
		grant.Principals = GrantPrincipals(rule.Principal)
		grant.NotPrincipals = GrantPrincipals(rule.NotPrincipal)
		for _, field := range []struct {
			Raw    json.RawMessage `json:"-"`
			Target *[]string       `json:"-"`
		}{{rule.Action, &grant.Actions}, {rule.NotAction, &grant.NotActions}, {rule.Resource, &grant.Resources}, {rule.NotResource, &grant.NotResources}} {
			values, err := identitiessvc.PolicyStrings(field.Raw)
			if err != nil {
				return nil, err
			}
			*field.Target = values
		}
		relevantAction := len(grant.NotActions) > 0
		for _, a := range grant.Actions {
			if a == "*" || strings.HasPrefix(a, "s3:") && a != "s3:ListAllMyBuckets" {
				relevantAction = true
			}
		}
		relevantResource := len(grant.NotResources) > 0
		for _, resource := range grant.Resources {
			if ResourceTouchesBucket(resource, bucket) {
				relevantResource = true
			}
		}
		if !relevantAction || !relevantResource {
			continue
		}
		grant.Uncertain = len(grant.NotActions) > 0 || len(grant.NotResources) > 0 || bytes.Contains(raw, []byte("${")) || len(rule.NotPrincipal) > 0
		result = append(result, grant)
	}
	return result, nil
}

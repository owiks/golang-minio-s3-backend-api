package identities

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/revision"
)

type policyStub struct {
	raw      []byte
	writes   int
	attached bool
}

func (f *policyStub) ListCannedPolicies(context.Context) (map[string]json.RawMessage, error) {
	return nil, nil
}
func (f *policyStub) InfoCannedPolicy(context.Context, string) ([]byte, error) { return f.raw, nil }
func (f *policyStub) AddCannedPolicy(context.Context, string, []byte) error    { f.writes++; return nil }
func (f *policyStub) RemoveCannedPolicy(context.Context, string) error         { f.writes++; return nil }
func (f *policyStub) GetPolicyEntities(context.Context, madmin.PolicyEntitiesQuery) (madmin.PolicyEntitiesResult, error) {
	var result madmin.PolicyEntitiesResult
	if f.attached {
		raw := []byte(`{"policyMappings":[{"policy":"read","users":["alice"]}]}`)
		if err := json.Unmarshal(raw, &result); err != nil {
			return result, err
		}
	}
	return result, nil
}
func TestPolicyUseCaseChecksRevisionAndDependenciesBeforeWriting(t *testing.T) {
	f := &policyStub{raw: []byte(`{"Version":"2012-10-17","Statement":[]}`)}
	input := UpdatePolicyCommand{Document: f.raw, Revision: "stale"}
	if err := UpdatePolicy(context.Background(), f, "read", input); apperror.Public(err).Code != "PolicyChanged" {
		t.Fatal(err)
	}
	if f.writes != 0 {
		t.Fatal("stale revision was written")
	}
	input.Revision = revision.Of(f.raw)
	if err := UpdatePolicy(context.Background(), f, "read", input); err != nil {
		t.Fatal(err)
	}
	f.attached = true
	if err := DeletePolicy(context.Background(), f, "read"); apperror.Public(err).Code != "PolicyInUse" {
		t.Fatal(err)
	}
	if f.writes != 1 {
		t.Fatal("assigned policy was deleted")
	}
}

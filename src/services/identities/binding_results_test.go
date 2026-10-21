package identities

import (
	"encoding/json"
	"github.com/minio/madmin-go/v3"
	"testing"
	"time"
)

func TestBindingDTOCompatibilityAndOwnership(t *testing.T) {
	for _, value := range []madmin.PolicyEntitiesResult{
		{},
		{Timestamp: time.Unix(1700000000, 0).UTC(), UserMappings: []madmin.UserPolicyEntities{{User: "alice", Policies: []string{"read"}, MemberOfMappings: []madmin.GroupPolicyEntities{{Group: "team", Policies: []string{"write"}}}}}, GroupMappings: []madmin.GroupPolicyEntities{{Group: "team", Policies: []string{"read"}}}, PolicyMappings: []madmin.PolicyEntities{{Policy: "read", Users: []string{"alice"}, Groups: []string{"team"}}}},
	} {
		dto := bindingResult(value)
		before, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		after, err := json.Marshal(dto)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatalf("public contract changed:\n%s\n%s", before, after)
		}
		if len(value.UserMappings) > 0 {
			value.UserMappings[0].Policies[0] = "changed"
			value.UserMappings[0].MemberOfMappings[0].Policies[0] = "changed"
			value.GroupMappings[0].Policies[0] = "changed"
			value.PolicyMappings[0].Users[0] = "changed"
			value.PolicyMappings[0].Groups[0] = "changed"
			detached, _ := json.Marshal(dto)
			if string(detached) != string(after) {
				t.Fatal("SDK mutation changed public DTO")
			}
		}
	}
}

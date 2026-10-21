package iam

import (
	"bytes"
	"encoding/json"
	"net"
	"sync"
	"testing"

	"github.com/minio/pkg/v3/policy"
	"github.com/minio/pkg/v3/policy/condition"
)

func nativePolicy(t *testing.T, document string) policy.Policy {
	t.Helper()
	p, err := policy.ParseConfig(bytes.NewBufferString(document))
	if err != nil {
		t.Fatal(err)
	}
	return *p
}

func TestIntersectOwnsEveryStatementCollection(t *testing.T) {
	const allow = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::data/uploads/*"]}]}`
	const notAction = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","NotAction":["s3:DeleteObject"],"Resource":["arn:aws:s3:::data/uploads/*"]}]}`
	const notResource = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"NotResource":["arn:aws:s3:::data/private/*"]}]}`
	const conditional = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::data/uploads/*"],"Condition":{"Null":{"aws:SourceIp":"true"}}}]}`
	for _, tc := range []struct {
		name, document, action, key string
		mutate                      func(*policy.Policy)
	}{
		{"effect", allow, "s3:GetObject", "uploads/file", func(p *policy.Policy) { p.Statements[0].Effect = policy.Deny }},
		{"replace statement", allow, "s3:GetObject", "uploads/file", func(p *policy.Policy) { p.Statements[0] = policy.Statement{} }},
		{"append statement", allow, "s3:GetObject", "uploads/file", func(p *policy.Policy) {
			p.Statements = append(p.Statements, policy.Statement{Effect: policy.Deny, Actions: policy.NewActionSet(policy.Action("s3:GetObject")), Resources: policy.NewResourceSet(policy.NewResource("data/*"))})
		}},
		{"add action", allow, "s3:PutObject", "uploads/file", func(p *policy.Policy) { p.Statements[0].Actions.Add(policy.Action("s3:PutObject")) }},
		{"delete action", allow, "s3:GetObject", "uploads/file", func(p *policy.Policy) {
			delete(p.Statements[0].Actions, policy.Action("s3:GetObject"))
			p.Statements[0].Actions.Add(policy.Action("s3:PutObject"))
		}},
		{"add not action", notAction, "s3:GetObject", "uploads/file", func(p *policy.Policy) { p.Statements[0].NotActions.Add(policy.Action("s3:GetObject")) }},
		{"delete not action", notAction, "s3:DeleteObject", "uploads/file", func(p *policy.Policy) { delete(p.Statements[0].NotActions, policy.Action("s3:DeleteObject")) }},
		{"add resource", allow, "s3:GetObject", "private/file", func(p *policy.Policy) { p.Statements[0].Resources.Add(policy.NewResource("data/private/*")) }},
		{"replace resource", allow, "s3:GetObject", "uploads/file", func(p *policy.Policy) {
			clear(p.Statements[0].Resources)
			p.Statements[0].Resources.Add(policy.NewResource("data/private/*"))
		}},
		{"add not resource", notResource, "s3:GetObject", "uploads/file", func(p *policy.Policy) { p.Statements[0].NotResources.Add(policy.NewResource("data/uploads/*")) }},
		{"delete not resource", notResource, "s3:GetObject", "private/file", func(p *policy.Policy) { clear(p.Statements[0].NotResources) }},
		{"replace condition", conditional, "s3:GetObject", "uploads/file", func(p *policy.Policy) {
			replacement := nativePolicy(t, `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::data/*"],"Condition":{"Null":{"aws:SourceIp":"false"}}}]}`)
			p.Statements[0].Conditions[0] = replacement.Statements[0].Conditions[0]
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := nativePolicy(t, tc.document)
			args := policy.Args{AccountName: "user", Action: policy.Action(tc.action), BucketName: "data", ObjectName: tc.key, ConditionValues: map[string][]string{}}
			want := input.IsAllowed(args)
			snapshot := Intersect(input)
			query := Query{Action: tc.action, Bucket: "data", Key: tc.key}
			if snapshot.Allows("user", query) != want {
				t.Fatal("copy changed the initial decision")
			}
			tc.mutate(&input)
			if input.IsAllowed(args) == want {
				t.Fatal("fixture did not change the input decision")
			}
			if got := snapshot.Allows("user", query); got != want {
				t.Fatal("mutating caller-owned data changed the published decision", want, got)
			}
		})
	}
}

func TestIntersectDeepCopiesConditionNetworkValues(t *testing.T) {
	_, network, err := net.ParseCIDR("10.1.2.0/24")
	if err != nil {
		t.Fatal(err)
	}
	values := []*net.IPNet{network}
	f, err := condition.NewIPAddressFunc(condition.AWSSourceIP.ToKey(), values...)
	if err != nil {
		t.Fatal(err)
	}
	input := policy.Policy{Version: policy.DefaultVersion, Statements: []policy.Statement{{Effect: policy.Allow, Actions: policy.NewActionSet(policy.Action("s3:GetObject")), Resources: policy.NewResourceSet(policy.NewResource("data/*")), Conditions: condition.Functions{f}}}}
	snapshot := Intersect(input)
	args := policy.Args{Action: policy.Action("s3:GetObject"), BucketName: "data", ObjectName: "file", ConditionValues: map[string][]string{"SourceIp": {"10.1.2.3"}}}
	if !input.IsAllowed(args) || !snapshot.layers[0].IsAllowed(args) {
		t.Fatal("invalid network fixture")
	}
	before, err := json.Marshal(snapshot.layers)
	if err != nil {
		t.Fatal(err)
	}
	network.IP[0] = 203
	network.Mask[3] = 255
	values[0] = &net.IPNet{IP: net.IPv4(192, 0, 2, 0), Mask: net.CIDRMask(24, 32)}
	after, err := json.Marshal(snapshot.layers)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !snapshot.layers[0].IsAllowed(args) || input.IsAllowed(args) {
		t.Fatal("condition still aliases caller-owned network or mask")
	}
}

func TestIntersectKeepsParentAndDerivedRestrictionsIndependent(t *testing.T) {
	parent := nativePolicy(t, `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject","s3:PutObject"],"Resource":["arn:aws:s3:::data/*"]}]}`)
	derived := nativePolicy(t, `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::data/public/*"]}]}`)
	inputs := []policy.Policy{parent, derived}
	first, second := Intersect(inputs...), Intersect(inputs...)
	parent.Statements[0].Effect = policy.Deny
	derived.Statements[0].Actions.Add(policy.Action("s3:PutObject"))
	derived.Statements[0].Resources.Add(policy.NewResource("data/private/*"))
	inputs[0], inputs[1] = policy.Policy{}, policy.Policy{}
	for _, snapshot := range []*Policy{first, second} {
		if !snapshot.Allows("user", Query{Action: "s3:GetObject", Bucket: "data", Key: "public/file"}) || snapshot.Allows("user", Query{Action: "s3:PutObject", Bucket: "data", Key: "public/file"}) || snapshot.Allows("user", Query{Action: "s3:GetObject", Bucket: "data", Key: "private/file"}) {
			t.Fatal("caller mutation changed a parent/derived intersection")
		}
	}
}

func TestIntersectConcurrentReadsAreDetachedFromInputMutations(t *testing.T) {
	input := nativePolicy(t, `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::data/*"]}]}`)
	snapshot := Intersect(input)
	var work sync.WaitGroup
	work.Go(func() {
		for range 1000 {
			input.Statements[0].Effect = policy.Deny
			input.Statements[0].Actions.Add(policy.Action("s3:PutObject"))
			input.Statements[0].Resources.Add(policy.NewResource("other/*"))
			input.Statements[0].Effect = policy.Allow
			delete(input.Statements[0].Actions, policy.Action("s3:PutObject"))
		}
	})
	for range 4 {
		work.Go(func() {
			for range 1000 {
				if !snapshot.Allows("user", Query{Action: "s3:GetObject", Bucket: "data", Key: "file"}) || snapshot.Allows("user", Query{Action: "s3:PutObject", Bucket: "data", Key: "file"}) {
					t.Error("published decision changed during input mutation")
					return
				}
			}
		})
	}
	work.Wait()
}

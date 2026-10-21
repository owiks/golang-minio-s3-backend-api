package iam

import (
	"testing"

	"github.com/minio/pkg/v3/policy"
)

func TestEmptyAndUnresolvedPoliciesFailClosed(t *testing.T) {
	q := Query{Action: "s3:PutObject", Bucket: "data", Key: "file.txt"}
	if Intersect().Allows("user", q) {
		t.Fatal("an empty intersection cannot prove permission")
	}
	for _, document := range []string{
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::data/${aws:username}/*"]}]}`,
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::data/*"]},{"Effect":"Deny","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::data/${aws:username}/*"]}]}`,
	} {
		layer, err := Parse([]byte(document))
		if err != nil {
			t.Fatal(err)
		}
		if Intersect(layer).Allows("user", q) {
			t.Fatal("unresolved substitution expanded permission")
		}
	}
}

func TestActionPolicyConservativeEvaluation(t *testing.T) {
	parent, e := Parse([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:PutObject","s3:DeleteObject"],"Resource":["arn:aws:s3:::data/uploads/*"]},{"Effect":"Deny","Action":["s3:DeleteObject"],"Resource":["arn:aws:s3:::data/uploads/keep*"],"Condition":{"Bool":{"aws:SecureTransport":"false"}}}]}`))
	if e != nil {
		t.Fatal(e)
	}
	p := Intersect(parent)
	for _, tc := range []struct {
		action, key string
		want        bool
	}{{"s3:PutObject", "uploads/file.txt", true}, {"s3:PutObject", "private/file.txt", false}, {"s3:DeleteObject", "uploads/file.txt", true}, {"s3:DeleteObject", "uploads/keep.txt", false}} {
		if got := p.Allows("user", Query{Action: tc.action, Bucket: "data", Key: tc.key}); got != tc.want {
			t.Fatalf("%+v got %v", tc, got)
		}
	}
	limited, e := Parse([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::data/uploads/only.txt"]}]}`))
	if e != nil {
		t.Fatal(e)
	}
	p = Intersect(parent, limited)
	if p.Allows("user", Query{Action: "s3:PutObject", Bucket: "data", Key: "uploads/file.txt"}) {
		t.Fatal("parent expanded derived key")
	}
	if !p.Allows("user", Query{Action: "s3:PutObject", Bucket: "data", Key: "uploads/only.txt"}) {
		t.Fatal("intersection missing")
	}
	conditional, e := Parse([]byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::data/*"],"Condition":{"Bool":{"aws:SecureTransport":"true"}}}]}`))
	if e != nil {
		t.Fatal(e)
	}
	if Intersect(conditional).Allows("u", Query{Action: "s3:PutObject", Bucket: "data", Key: "a"}) {
		t.Fatal("conditional allow guessed")
	}
	if (*Policy)(nil).Allows("u", Query{Action: "admin:CreateUser"}) {
		t.Fatal("unknown expanded")
	}
}

func TestFilteredPolicyDoesNotRetainOriginalStatementIndexes(t *testing.T) {
	for _, condition := range []string{
		`"Resource":["arn:aws:s3:::data/${aws:username}/*"]`,
		`"Resource":["arn:aws:s3:::data/*"],"Condition":{"Bool":{"aws:SecureTransport":"true"}}`,
	} {
		document := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],` + condition + `},{"Effect":"Allow","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::data/*"]},{"Effect":"Allow","Action":["s3:DeleteObject"],"Resource":["arn:aws:s3:::data/*"]}]}`
		layer, err := Parse([]byte(document))
		if err != nil {
			t.Fatal(err)
		}
		for _, action := range []string{"s3:GetObject", "s3:PutObject", "s3:DeleteObject"} {
			want := action != "s3:GetObject"
			if got := Intersect(layer).Allows("user", Query{Action: action, Bucket: "data", Key: "file"}); got != want {
				t.Fatal("filtering changed an unrelated statement or retained the discarded allow", action, got)
			}
		}
	}
}

func TestIntersectDoesNotCopyIndexesFromEditedSDKPolicy(t *testing.T) {
	input := nativePolicy(t, `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::data/*"]},{"Effect":"Allow","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::data/*"]}]}`)
	input.Statements = input.Statements[1:]
	snapshot := Intersect(input)
	if !snapshot.Allows("user", Query{Action: "s3:PutObject", Bucket: "data", Key: "file"}) || snapshot.Allows("user", Query{Action: "s3:GetObject", Bucket: "data", Key: "file"}) {
		t.Fatal("SDK indexes overrode the actual statements in the snapshot")
	}
	for _, statements := range [][]policy.Statement{nil, {}} {
		input.Statements = statements
		for _, layers := range [][]policy.Policy{{input}, {input, snapshot.layers[0]}, {snapshot.layers[0], input}} {
			if Intersect(layers...).Allows("user", Query{Action: "s3:PutObject", Bucket: "data", Key: "file"}) {
				t.Fatal("an empty layer expanded the intersection")
			}
		}
	}
}

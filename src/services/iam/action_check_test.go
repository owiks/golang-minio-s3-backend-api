package iam

import (
	"context"
	"testing"

	"github.com/minio/madmin-go/v3"
)

type keyInspection struct {
	calls int
	info  madmin.InfoAccessKeyResp
	err   error
}

func (f *keyInspection) InfoAccessKey(context.Context, string) (madmin.InfoAccessKeyResp, error) {
	f.calls++
	return f.info, f.err
}

func TestDerivedKeyHintsNeverFallBackToParentRights(t *testing.T) {
	parent := madmin.AccountInfo{AccountName: "parent", Policy: []byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:*"],"Resource":["arn:aws:s3:::*"]}]}`)}
	readOnly := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::reports/*"]}]}`
	for _, kind := range []string{"restricted", "inherited", "denied", "wrong-parent", "disabled", "invalid-policy", "missing-identity", "invalid-parent-policy", "parent"} {
		t.Run(kind, func(t *testing.T) {
			client := &keyInspection{}
			client.info.ParentUser = "parent"
			client.info.AccountStatus = "on"
			client.info.Policy = readOnly
			account, actor := parent, "derived"
			expected := ""
			switch kind {
			case "inherited":
				client.info.ImpliedPolicy = true
			case "denied":
				client.err = madmin.ErrorResponse{Code: "AccessDenied"}
				expected = "AccessKeyInfoDenied"
			case "wrong-parent":
				client.info.ParentUser = "other"
				expected = "AccessKeyParentMismatch"
			case "disabled":
				client.info.AccountStatus = "off"
				expected = "AccessKeyDisabled"
			case "invalid-policy":
				client.info.Policy = "invalid"
				expected = "AccessKeyPolicyInvalid"
			case "missing-identity":
				account.AccountName = ""
				expected = "AccountIdentityMissing"
			case "invalid-parent-policy":
				account.Policy = []byte("invalid")
				expected = "AccountPolicyInvalid"
			case "parent":
				actor = "parent"
			}
			value, issue := ResolveActionPolicy(context.Background(), client, actor, account)
			if expected != "" {
				if value != nil || issue == nil || issue.Code != expected {
					t.Fatal("unconfirmed restrictions granted rights", value, issue)
				}
				return
			}
			if issue != nil || !value.Allows(actor, Query{Action: "s3:GetObject", Bucket: "reports", Key: "file"}) {
				t.Fatal("valid read rights lost", issue)
			}
			canWrite := value.Allows(actor, Query{Action: "s3:PutObject", Bucket: "reports", Key: "file"})
			if canWrite != (kind != "restricted") {
				t.Fatal("derived restrictions lost")
			}
			if kind == "parent" && client.calls != 0 {
				t.Fatal("parent unexpectedly inspected as derived key")
			}
		})
	}
}

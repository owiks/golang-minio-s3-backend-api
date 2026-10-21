package iam

import (
	"context"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/pkg/v3/policy"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

type ActionCheckIssue struct {
	Code       string       `json:"code"`
	Message    messages.Ref `json:"message"`
	HTTPStatus int          `json:"httpStatus,omitempty"`
	Cause      string       `json:"cause,omitempty"`
}

func ActionReadIssue(stage string, err error) *ActionCheckIssue {
	code := apperror.Public(err).Code
	switch code {
	case "Timeout":
		return &ActionCheckIssue{Code: "ActionCheckTimeout", Message: messages.IamMinIODidNotReturnActionPermissionsIn}
	case "AccessDenied":
		return &ActionCheckIssue{Code: stage + "Denied", Message: messages.IamMinIODeniedReadingActionPermissionsModificationButtons}
	case "NotImplemented", "XMinioAdminNotImplemented":
		return &ActionCheckIssue{Code: stage + "Unsupported", Message: messages.IamTheServerDoesNotSupportCheckingAction}
	default:
		return &ActionCheckIssue{Code: stage + "Unavailable", Message: messages.IamCouldNotRetrieveActionPermissionsFromMinIO}
	}
}

// ActionPolicyClient exposes only inspection of a derived key, never its secret.
type ActionPolicyClient interface {
	InfoAccessKey(context.Context, string) (madmin.InfoAccessKeyResp, error)
}

// ResolveActionPolicy computes conservative UI hints. MinIO authorizes every
// operation itself; an unconfirmed derived-key restriction never inherits Allow.
func ResolveActionPolicy(ctx context.Context, client ActionPolicyClient, actor string, account madmin.AccountInfo) (*Policy, *ActionCheckIssue) {
	if account.AccountName == "" {
		return nil, &ActionCheckIssue{Code: "AccountIdentityMissing", Message: messages.IamMinIODidNotReturnTheConnectionOwner}
	}
	parent, err := Parse(account.Policy)
	if err != nil {
		return nil, &ActionCheckIssue{Code: "AccountPolicyInvalid", Message: messages.IamCouldNotReadTheIAMPolicyFormat}
	}
	layers := []policy.Policy{parent}
	if account.AccountName != actor {
		// AccountInfo describes the parent. A derived key must also pass its own
		// restrictions; never replace failed inspection with its parent's rights.
		info, err := client.InfoAccessKey(ctx, actor)
		if err != nil {
			return nil, ActionReadIssue("AccessKeyInfo", err)
		}
		if info.ParentUser != account.AccountName {
			return nil, &ActionCheckIssue{Code: "AccessKeyParentMismatch", Message: messages.IamCouldNotConfirmTheKeyOwnerModification}
		}
		if info.AccountStatus != "on" && info.AccountStatus != "enabled" {
			return nil, &ActionCheckIssue{Code: "AccessKeyDisabled", Message: messages.IamMinIOReportsThatTheKeyIsDisabled}
		}
		if !info.ImpliedPolicy {
			restricted, err := Parse([]byte(info.Policy))
			if err != nil {
				return nil, &ActionCheckIssue{Code: "AccessKeyPolicyInvalid", Message: messages.IamCouldNotReadTheKeyRestrictionsModification}
			}
			layers = append(layers, restricted)
		}
	}
	return Intersect(layers...), nil
}

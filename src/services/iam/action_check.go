package iam

import (
	"context"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/pkg/v3/policy"

	"minio-web/api/src/services/apperror"
)

type ActionCheckIssue struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	Cause      string `json:"cause,omitempty"`
}

func ActionReadIssue(stage string, err error) *ActionCheckIssue {
	code := apperror.Public(err).Code
	switch code {
	case "Timeout":
		return &ActionCheckIssue{Code: "ActionCheckTimeout", Message: "MinIO не успел вернуть права на действия. Кнопки изменения скрыты."}
	case "AccessDenied":
		return &ActionCheckIssue{Code: stage + "Denied", Message: "MinIO не разрешил прочитать права на действия. Кнопки изменения скрыты."}
	case "NotImplemented", "XMinioAdminNotImplemented":
		return &ActionCheckIssue{Code: stage + "Unsupported", Message: "Сервер не поддерживает проверку прав на действия. Кнопки изменения скрыты."}
	default:
		return &ActionCheckIssue{Code: stage + "Unavailable", Message: "Не удалось получить права на действия из MinIO. Кнопки изменения скрыты."}
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
		return nil, &ActionCheckIssue{Code: "AccountIdentityMissing", Message: "MinIO не вернул владельца подключения. Кнопки изменения скрыты."}
	}
	parent, err := Parse(account.Policy)
	if err != nil {
		return nil, &ActionCheckIssue{Code: "AccountPolicyInvalid", Message: "Не удалось прочитать формат IAM-политики. Кнопки изменения скрыты."}
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
			return nil, &ActionCheckIssue{Code: "AccessKeyParentMismatch", Message: "Не удалось подтвердить владельца ключа. Кнопки изменения скрыты."}
		}
		if info.AccountStatus != "on" && info.AccountStatus != "enabled" {
			return nil, &ActionCheckIssue{Code: "AccessKeyDisabled", Message: "MinIO сообщает, что ключ отключён. Кнопки изменения скрыты."}
		}
		if !info.ImpliedPolicy {
			restricted, err := Parse([]byte(info.Policy))
			if err != nil {
				return nil, &ActionCheckIssue{Code: "AccessKeyPolicyInvalid", Message: "Не удалось прочитать ограничения ключа. Кнопки изменения скрыты."}
			}
			layers = append(layers, restricted)
		}
	}
	return Intersect(layers...), nil
}

package keys

import (
	"bytes"
	"context"
	"sort"
	"sync"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/apperror"
	validationsvc "minio-web/api/src/services/validation"
)

type GeneratedKey struct {
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
}
type StatusRequest struct {
	Enabled  *bool  `json:"enabled" validate:"required"`
	Revision string `json:"revision" validate:"required"`
}
type RotateRequest struct {
	Confirmation string `json:"confirmation" validate:"required"`
	Revision     string `json:"revision" validate:"required"`
}

func ListKeys(ctx context.Context, s ListKeysClient, temporary bool, scope, parent string) ([]AccessKeyRow, error) {
	if scope == "" {
		scope = "own"
	}
	if scope != "own" && scope != "all" && scope != "user" || scope == "user" && !validationsvc.ValidName(parent) || scope != "user" && parent != "" {
		return nil, apperror.New(400, "InvalidScope", "Выберите свои ключи, все ключи или одного пользователя.")
	}
	rows := make([]AccessKeyRow, 0)
	if !temporary && scope != "all" {
		result, err := s.ListServiceAccounts(ctx, parent)
		if err != nil {
			return nil, err
		}
		for _, info := range result.Accounts {
			rows = append(rows, KeyRow(s, info))
		}
	} else {
		var parents []string
		if scope == "user" {
			parents = []string{parent}
		}
		listType := madmin.AccessKeyListSvcaccOnly
		if temporary {
			listType = madmin.AccessKeyListSTSOnly
		}
		result, err := s.ListAccessKeysBulk(ctx, parents, madmin.ListAccessKeysOpts{ListType: listType, All: scope == "all"})
		if err != nil {
			return nil, err
		}
		for owner, accounts := range result {
			items := accounts.ServiceAccounts
			if temporary {
				items = accounts.STSKeys
			}
			for _, info := range items {
				if info.ParentUser == "" {
					info.ParentUser = owner
				}
				rows = append(rows, KeyRow(s, info))
			}
		}
		if !temporary {
			// Older MinIO bulk responses contain only IDs and expiry. Fetch the
			// full list per owner so names and statuses are not silently lost.
			owners := make([]string, 0, len(result))
			for owner := range result {
				owners = append(owners, owner)
			}
			sort.Strings(owners)
			rows = rows[:0]
			for offset := 0; offset < len(owners); offset += 4 {
				batch := owners[offset:min(offset+4, len(owners))]
				values := make([]madmin.ListServiceAccountsResp, len(batch))
				failures := make([]error, len(batch))
				var wait sync.WaitGroup
				for i, owner := range batch {
					wait.Go(func() { values[i], failures[i] = s.ListServiceAccounts(ctx, owner) })
				}
				wait.Wait()
				for i, value := range values {
					if failures[i] != nil {
						return nil, failures[i]
					}
					for _, info := range value.Accounts {
						rows = append(rows, KeyRow(s, info))
					}
				}
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].AccessKey < rows[j].AccessKey })
	return rows, nil
}
func CreateKey(ctx context.Context, s CreateKeyClient, b KeyInput) (GeneratedKey, error) {
	if err := ValidateKeyInput(b); err != nil {
		return GeneratedKey{}, err
	}
	policy := b.Policy
	if bytes.Equal(bytes.TrimSpace(policy), []byte("null")) {
		policy = nil
	}
	value, err := s.AddServiceAccount(ctx, madmin.AddServiceAccountReq{TargetUser: b.Parent, Name: b.Name, Description: b.Description, Expiration: b.Expiration, Policy: policy})
	if err != nil {
		return GeneratedKey{}, err
	}
	return GeneratedKey{AccessKey: value.AccessKey, SecretKey: value.SecretKey}, nil
}
func EnsureMutable(s EnsureMutableClient, name string) error {
	if name == s.ActorName() {
		return apperror.New(409, "CurrentAccessKey", "Нельзя менять или удалять ключ текущего подключения. Подключитесь с другим ключом.")
	}
	return nil
}
func DeleteKey(ctx context.Context, s DeleteKeyClient, name string) error {
	if err := EnsureMutable(s, name); err != nil {
		return err
	}
	return s.DeleteServiceAccount(ctx, name)
}

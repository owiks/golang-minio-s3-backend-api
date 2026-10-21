package keys

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/apperror"
)

func EditKey(ctx context.Context, s ChangeKeyClient, name string, input KeyInput) error {
	if err := EnsureMutable(s, name); err != nil {
		return err
	}
	check := input
	check.Expiration = nil // An unchanged deadline may already be close to expiry.
	if err := ValidateKeyInput(check); err != nil {
		return err
	}
	if input.Parent != "" {
		return apperror.New(400, "ImmutableParent", "Владельца существующего ключа нельзя изменить. Создайте новый ключ у нужного пользователя.")
	}
	policy := input.Policy
	if bytes.Equal(bytes.TrimSpace(policy), []byte("null")) {
		policy = json.RawMessage(`{}`)
	}
	expiration := input.Expiration
	if expiration == nil {
		zero := time.Unix(0, 0).UTC()
		expiration = &zero
	}
	opts := madmin.UpdateServiceAccountReq{
		NewName: input.Name, NewDescription: input.Description,
		NewPolicy: policy, NewExpiration: expiration,
	}
	current, err := keyForChange(ctx, s, name, input.Revision)
	if err != nil {
		return err
	}
	unchanged := input.Expiration == nil && current.Expiration == nil || input.Expiration != nil && current.Expiration != nil && input.Expiration.Equal(*current.Expiration)
	if unchanged {
		opts.NewExpiration = nil
	} else if err := ValidateKeyExpiration(input.Expiration); err != nil {
		return err
	}
	if input.Name == "" && current.Name != "" || input.Description == "" && current.Description != "" {
		return apperror.New(400, "EmptyKeyMetadata", "Чтобы изменить имя или описание, укажите новое значение. Очистка этих полей не поддерживается этой операцией MinIO.")
	}
	return s.UpdateServiceAccount(ctx, name, opts)
}

package keys

import (
	"context"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/securetoken"
)

func RotateKeySecret(ctx context.Context, s ChangeKeyClient, name string, input RotateRequest) (GeneratedKey, error) {
	if err := EnsureMutable(s, name); err != nil {
		return GeneratedKey{}, err
	}
	if input.Confirmation != name {
		return GeneratedKey{}, apperror.New(400, "ConfirmationRequired", "Для смены секрета введите Access Key.")
	}
	current, err := keyForChange(ctx, s, name, input.Revision)
	if err != nil {
		return GeneratedKey{}, err
	}
	generated := securetoken.New()
	opts := madmin.UpdateServiceAccountReq{NewSecretKey: generated}
	// Some MinIO releases remove the inline policy when NewPolicy is omitted.
	// Rotation must never expand the key's permissions.
	if !current.Inherited {
		opts.NewPolicy = current.Policy
	}
	if err := s.UpdateServiceAccount(ctx, name, opts); err != nil {
		return GeneratedKey{}, err
	}
	return GeneratedKey{AccessKey: name, SecretKey: generated}, nil
}

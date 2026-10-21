package keys

import (
	"context"

	"github.com/minio/madmin-go/v3"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

func RotateKeySecret(ctx context.Context, s ChangeKeyClient, name string, input RotateRequest) (GeneratedKey, error) {
	if err := EnsureMutable(s, name); err != nil {
		return GeneratedKey{}, err
	}
	if input.Confirmation != name {
		return GeneratedKey{}, apperror.New(400, "ConfirmationRequired", messages.KeysConfirmationRequired)
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

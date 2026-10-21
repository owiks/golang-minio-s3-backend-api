package keys

import (
	"context"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/apperror"
)

func SetKeyStatus(ctx context.Context, s ChangeKeyClient, name string, input StatusRequest) error {
	if err := EnsureMutable(s, name); err != nil {
		return err
	}
	if input.Enabled == nil {
		return apperror.New(400, "InvalidStatus", "Укажите enabled.")
	}
	opts := madmin.UpdateServiceAccountReq{NewStatus: "off"}
	if *input.Enabled {
		opts.NewStatus = "on"
	}
	current, err := keyForChange(ctx, s, name, input.Revision)
	if err != nil {
		return err
	}
	// Some MinIO releases remove the inline policy when NewPolicy is omitted.
	// A status change must never expand the key's permissions.
	if !current.Inherited {
		opts.NewPolicy = current.Policy
	}
	return s.UpdateServiceAccount(ctx, name, opts)
}

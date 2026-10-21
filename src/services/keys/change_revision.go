package keys

import (
	"context"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

func keyForChange(ctx context.Context, s ServiceKeyReader, name, expected string) (AccessKeyDetails, error) {
	current, err := serviceKeyDetail(ctx, s, name)
	if err != nil {
		return AccessKeyDetails{}, err
	}
	if expected == "" || current.Revision != expected {
		return AccessKeyDetails{}, apperror.New(409, "AccessKeyChanged", messages.KeysAccessKeyChanged)
	}
	return current, nil
}

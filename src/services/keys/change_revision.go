package keys

import (
	"context"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
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

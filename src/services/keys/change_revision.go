package keys

import (
	"context"

	"minio-web/api/src/services/apperror"
)

func keyForChange(ctx context.Context, s ServiceKeyReader, name, expected string) (AccessKeyDetails, error) {
	current, err := serviceKeyDetail(ctx, s, name)
	if err != nil {
		return AccessKeyDetails{}, err
	}
	if expected == "" || current.Revision != expected {
		return AccessKeyDetails{}, apperror.New(409, "AccessKeyChanged", "Ключ изменился после открытия окна. Ваши правки не записаны. Закройте окно и откройте актуальные настройки.")
	}
	return current, nil
}

package buckets

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/tags"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/revision"
)

type QuotaSetting struct {
	Size     uint64           `json:"size"`
	Type     madmin.QuotaType `json:"type"`
	Rate     uint64           `json:"rate"`
	Requests uint64           `json:"requests"`
}

func SettingField(value any, err error) DetailField {
	f := Detail(value, err)
	if err == nil {
		raw, _ := json.Marshal(value)
		f.Revision = revision.Of(raw)
	}
	return f
}
func ReadBucketSetting(ctx context.Context, s ReadBucketSettingClient, bucket, kind string) (SettingValue, error) {
	switch kind {
	case "versioning":
		return ReadVersioning(ctx, s, bucket)
	case "locking":
		return ReadLocking(ctx, s, bucket)
	case "policy":
		document, err := ReadPolicy(ctx, s, bucket)
		return policySetting(document), err
	case "quota":
		return ReadQuota(ctx, s, bucket)
	case "tags":
		return ReadTags(ctx, s, bucket)
	}
	return nil, apperror.New(405, "MethodNotAllowed", "Эта настройка не поддерживается.")
}
func EnsureSettingRevision(current any, expected string) error {
	if expected == "" || SettingField(current, nil).Revision != expected {
		return apperror.New(409, "BucketSettingChanged", "Настройка изменилась после открытия. Ваши правки не записаны. Загрузите актуальное значение перед повторным сохранением.")
	}
	return nil
}

type TagInput struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func ValidateBucketTags(items []TagInput) (*tags.Tags, error) {
	if items == nil || len(items) > 50 {
		return nil, apperror.New(400, "InvalidTags", "Передайте список не более 50 меток. Для удаления всех меток передайте пустой список.")
	}
	values := map[string]string{}
	for _, item := range items {
		if _, found := values[item.Key]; found {
			return nil, apperror.New(400, "InvalidTags", "Ключи меток не должны повторяться.")
		}
		if strings.HasPrefix(strings.ToLower(item.Key), "aws:") {
			return nil, apperror.New(400, "InvalidTags", "Префикс aws: зарезервирован. Выберите другой ключ метки.")
		}
		values[item.Key] = item.Value
	}
	value, err := tags.NewTags(values, false)
	if err != nil {
		return nil, apperror.New(400, "InvalidTags", "Проверьте метки: ключ 1–128 символов, значение до 256. Допустимы латинские буквы, цифры, пробелы и символы + - . _ : / @ =.")
	}
	return value, nil
}

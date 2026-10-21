package buckets

import (
	"context"
	"strings"
	"unicode"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/cors"
	"github.com/minio/minio-go/v7/pkg/sse"

	"minio-web/api/src/services/apperror"
)

type BrowserChangeRequest struct {
	Revision     string    `json:"revision"`
	Index        *int      `json:"index"`
	Rule         *CorsRule `json:"rule"`
	Algorithm    string    `json:"algorithm"`
	KeyID        string    `json:"keyId"`
	Confirmation string    `json:"confirmation"`
}

func ChangeBrowserSetting(ctx context.Context, s ChangeBrowserSettingsClient, bucket, kind string, operation string, body BrowserChangeRequest) (DetailField, error) {
	state, err := ReadBrowserSetting(ctx, s, bucket, kind)
	if err != nil {
		return DetailField{}, err
	}
	if body.Revision == "" || body.Revision != state.Revision {
		return DetailField{}, apperror.New(409, "BucketSettingChanged", "Настройка изменилась. Загрузите актуальное значение перед сохранением.")
	}
	if !state.Value.Writable {
		return DetailField{}, apperror.New(409, "UnsupportedBucketSetting", state.Value.Warning)
	}
	var status int
	if kind == "cors" {
		rules := state.Cors.CORSRules
		index := -1
		if operation != "create" {
			if body.Index == nil || *body.Index < 0 || *body.Index >= len(rules) {
				return DetailField{}, apperror.New(404, "CORSRuleNotFound", "Правило CORS больше не существует. Обновите список.")
			}
			index = *body.Index
		}
		switch operation {
		case "create", "update":
			if body.Rule == nil {
				return DetailField{}, apperror.New(400, "InvalidCORS", "Передайте правило CORS.")
			}
			if err := ValidateCorsRule(*body.Rule); err != nil {
				return DetailField{}, err
			}
			if index < 0 && len(rules) >= 100 {
				return DetailField{}, apperror.New(400, "InvalidCORS", "В бакете допускается не более 100 правил CORS.")
			}
			input := body.Rule
			for i, rule := range rules {
				if input.ID != "" && rule.ID == input.ID && i != index {
					return DetailField{}, apperror.New(409, "CORSDuplicateID", "Правило с таким именем уже существует.")
				}
			}
			parsed := cors.Rule{ID: input.ID, AllowedOrigin: input.Origins, AllowedMethod: input.Methods, AllowedHeader: input.Headers, ExposeHeader: input.Expose, MaxAgeSeconds: input.MaxAge}
			if index < 0 {
				rules = append(rules, parsed)
			} else {
				rules[index] = parsed
			}
		case "delete":
			if body.Confirmation != "delete" {
				return DetailField{}, apperror.New(400, "ConfirmationRequired", "Подтвердите удаление правила CORS.")
			}
			rules = append(rules[:index], rules[index+1:]...)
		default:
			return DetailField{}, apperror.New(405, "MethodNotAllowed", "Метод не поддерживается.")
		}
		var cfg *cors.Config
		if len(rules) > 0 {
			cfg = cors.NewConfig(rules)
		}
		status, err = s.WriteCORSConfig(ctx, bucket, cfg)
	} else {
		if operation != "update" {
			return DetailField{}, apperror.New(405, "MethodNotAllowed", "Метод не поддерживается.")
		}
		if strings.ContainsFunc(body.KeyID, unicode.IsControl) || len(body.KeyID) > 1024 {
			return DetailField{}, apperror.New(400, "InvalidEncryption", "ID ключа: до 1024 символов без управляющих символов.")
		}
		switch body.Algorithm {
		case "":
			if body.Confirmation != "remove-default" {
				return DetailField{}, apperror.New(400, "ConfirmationRequired", "Подтвердите снятие шифрования по умолчанию.")
			}
			status, err = s.WriteEncryptionConfig(ctx, bucket, nil)
		case "AES256":
			if body.KeyID != "" {
				return DetailField{}, apperror.New(400, "InvalidEncryption", "SSE-S3 не принимает ID ключа.")
			}
			status, err = s.WriteEncryptionConfig(ctx, bucket, sse.NewConfigurationSSES3())
		case "aws:kms":
			status, err = s.WriteEncryptionConfig(ctx, bucket, sse.NewConfigurationSSEKMS(body.KeyID))
		default:
			return DetailField{}, apperror.New(400, "InvalidEncryption", "Выберите SSE-S3 или SSE-KMS.")
		}
	}
	if err != nil {
		switch minio.ToErrorResponse(err).Code {
		case "KMSNotConfigured", "KMSKeyNotFound", "KMSAccessDeniedException", "kms:KeyNotFound", "kms:NotSupported":
			return DetailField{}, apperror.New(409, "KMSUnavailable", "MinIO не смог использовать KMS. Проверьте подключение KMS и ID существующего ключа.")
		case "InvalidArgument", "MalformedXML":
			return DetailField{}, apperror.New(400, "InvalidBucketSetting", "MinIO отклонил настройку. Проверьте параметры.")
		}
		return DetailField{}, err
	}
	if status < 200 || status >= 300 {
		if status == 403 {
			return DetailField{}, apperror.New(403, "AccessDenied", "Недостаточно прав MinIO для этой операции.")
		}
		return DetailField{}, apperror.New(502, "BucketSettingWriteFailed", "MinIO не подтвердил изменение. Обновите сведения.")
	}
	stored, readErr := ReadBrowserSetting(ctx, s, bucket, kind)
	return BrowserSettingField(stored, readErr), nil
}

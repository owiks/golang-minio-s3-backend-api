package keys

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"time"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/apperror"
	identitiessvc "minio-web/api/src/services/identities"
	"minio-web/api/src/services/revision"
	validationsvc "minio-web/api/src/services/validation"
)

type AccessKeyRow struct {
	AccessKey   string     `json:"accessKey"`
	Parent      string     `json:"parent"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Inherited   bool       `json:"inherited"`
	Expiration  *time.Time `json:"expiration"`
	Current     bool       `json:"current"`
}
type AccessKeyDetails struct {
	AccessKeyRow

	Policy   json.RawMessage `json:"policy" validate:"required"`
	Revision string          `json:"revision"`
}

func KeyExpiration(value *time.Time) *time.Time {
	if value == nil || value.IsZero() || value.Equal(time.Unix(0, 0)) {
		return nil
	}
	return value
}
func KeyRow(s EnsureMutableClient, info madmin.ServiceAccountInfo) AccessKeyRow {
	return AccessKeyRow{AccessKey: info.AccessKey, Parent: info.ParentUser, Name: info.Name, Description: info.Description, Status: info.AccountStatus, Inherited: info.ImpliedPolicy, Expiration: KeyExpiration(info.Expiration), Current: info.AccessKey == s.ActorName()}
}
func KeyDetail(ctx context.Context, s KeyDetailClient, key string, temporary bool) (AccessKeyDetails, error) {
	if !temporary {
		return serviceKeyDetail(ctx, s, key)
	}
	value, err := s.TemporaryAccountInfo(ctx, key)
	return keyDetail(s, key, madmin.InfoServiceAccountResp(value), err)
}

func serviceKeyDetail(ctx context.Context, s ServiceKeyReader, key string) (AccessKeyDetails, error) {
	info, err := s.InfoServiceAccount(ctx, key)
	return keyDetail(s, key, info, err)
}

func keyDetail(s EnsureMutableClient, key string, info madmin.InfoServiceAccountResp, err error) (AccessKeyDetails, error) {
	if err != nil {
		if identitiessvc.Missing(err, "XMinioInvalidIAMCredentials") {
			return AccessKeyDetails{}, apperror.New(404, "AccessKeyNotFound", "Ключ больше не существует или недоступен этому подключению. Обновите список.")
		}
		return AccessKeyDetails{}, err
	}
	policy := json.RawMessage("null")
	if !info.ImpliedPolicy {
		if !json.Valid([]byte(info.Policy)) || bytes.Equal(bytes.TrimSpace([]byte(info.Policy)), []byte("null")) {
			return AccessKeyDetails{}, apperror.New(502, "UnreadableAccessKeyPolicy", "MinIO не вернул политику ключа. Настройки нельзя менять, пока политика не прочитана.")
		}
		policy = json.RawMessage(info.Policy)
	}
	value := AccessKeyDetails{AccessKeyRow: KeyRow(s, madmin.ServiceAccountInfo{AccessKey: key, ParentUser: info.ParentUser, Name: info.Name, Description: info.Description, AccountStatus: info.AccountStatus, ImpliedPolicy: info.ImpliedPolicy, Expiration: info.Expiration}), Policy: policy}
	raw, _ := json.Marshal(value)
	value.Revision = revision.Of(raw)
	return value, nil
}

var KeyNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,31}$`)

type KeyInput struct {
	Parent      string          `json:"parent,omitempty"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Expiration  *time.Time      `json:"expiration"`
	Policy      json.RawMessage `json:"policy" validate:"required"`
	Revision    string          `json:"revision,omitempty"`
}

func ValidateKeyInput(b KeyInput) error {
	if b.Name != "" && !KeyNamePattern.MatchString(b.Name) {
		return apperror.New(400, "InvalidKeyName", "Имя ключа: до 32 латинских букв, цифр, дефисов и подчёркиваний. Первый символ — буква.")
	}
	if len(b.Description) > 256 {
		return apperror.New(400, "InvalidKeyDescription", "Описание должно занимать не более 256 байт UTF-8.")
	}
	if b.Parent != "" && !validationsvc.ValidName(b.Parent) {
		return apperror.New(400, "InvalidParent", "Укажите корректного пользователя-владельца.")
	}
	if err := ValidateKeyExpiration(b.Expiration); err != nil {
		return err
	}
	if len(b.Policy) == 0 {
		return apperror.New(400, "InvalidPolicy", "Укажите политику или null для наследования прав пользователя.")
	}
	if !bytes.Equal(bytes.TrimSpace(b.Policy), []byte("null")) {
		if err := validationsvc.ValidateDocument(b.Policy); err != nil {
			return err
		}
		var doc struct {
			Statement json.RawMessage `json:"Statement"`
		}
		_ = json.Unmarshal(b.Policy, &doc)
		statement := bytes.TrimSpace(doc.Statement)
		var rules []json.RawMessage
		if len(statement) > 0 && statement[0] == '{' {
			rules = []json.RawMessage{statement}
		} else if json.Unmarshal(statement, &rules) != nil {
			return apperror.New(400, "InvalidPolicy", "Statement должен содержать правило или список правил доступа.")
		}
		if len(rules) == 0 {
			return apperror.New(400, "InvalidPolicy", "Добавьте хотя бы одно правило. Для полного запрета используйте правило с Effect: Deny.")
		}
	}
	return nil
}
func ValidateKeyExpiration(value *time.Time) error {
	if value != nil && (value.Before(time.Now().Add(15*time.Minute)) || value.After(time.Now().Add(365*24*time.Hour))) {
		return apperror.New(400, "InvalidExpiration", "Выберите срок от 15 минут до 365 дней. Для бессрочного ключа выберите «Без срока».")
	}
	return nil
}

package server

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/iam"
	"minio-web/api/src/services/revision"
	validationsvc "minio-web/api/src/services/validation"
)

type ConfigField struct {
	Key       string   `json:"key"`
	Label     string   `json:"label"`
	Kind      string   `json:"kind"`
	Value     string   `json:"value"`
	Env       string   `json:"env,omitempty"`
	Effective string   `json:"effective"`
	Options   []string `json:"options,omitempty"`
}

// Definitions are constructed per call: neither fields nor nested Options share
// mutable state with another read or with subsequent command validation.
func configDefinitions(sub string) []ConfigField {
	switch sub {
	case "api":
		return []ConfigField{
			{Key: "requests_max", Label: "Одновременные запросы (0 — автоматически)", Kind: "number"},
			{Key: "cluster_deadline", Label: "Ожидание ответа кластера", Kind: "duration"},
			{Key: "remote_transport_deadline", Label: "Ожидание удалённого хранилища", Kind: "duration"},
			{Key: "replication_priority", Label: "Приоритет репликации", Kind: "select", Options: []string{"auto", "fast", "slow"}},
			{Key: "replication_max_workers", Label: "Обработчики репликации", Kind: "number"},
			{Key: "replication_max_lrg_workers", Label: "Обработчики крупных объектов", Kind: "number"},
			{Key: "transition_workers", Label: "Обработчики переноса между уровнями", Kind: "number"},
			{Key: "stale_uploads_cleanup_interval", Label: "Интервал очистки прерванных загрузок", Kind: "duration"},
			{Key: "stale_uploads_expiry", Label: "Возраст прерванных загрузок для очистки", Kind: "duration"},
			{Key: "delete_cleanup_interval", Label: "Интервал фоновой очистки удалений", Kind: "duration"},
		}
	case "scanner":
		return []ConfigField{
			{Key: "speed", Label: "Скорость сканера", Kind: "select", Options: []string{"slowest", "slow", "default", "fast", "fastest"}},
			{Key: "alert_excess_versions", Label: "Порог числа версий объекта", Kind: "number"},
			{Key: "alert_excess_folders", Label: "Порог числа папок", Kind: "number"},
		}
	case "heal":
		return []ConfigField{
			{Key: "bitrotscan", Label: "Проверка повреждений", Kind: "select", Options: []string{"off", "on"}},
			{Key: "max_sleep", Label: "Пауза восстановления", Kind: "duration"},
			{Key: "max_io", Label: "Порог операций ввода-вывода", Kind: "number"},
			{Key: "drive_workers", Label: "Обработчики диска (пусто — автоматически)", Kind: "number"},
		}
	default:
		return nil
	}
}

func validateConfigSubsystem(sub string) error {
	if configDefinitions(sub) == nil {
		return apperror.New(400, "InvalidSubsystem", "Выберите API, сканер или восстановление.")
	}
	return nil
}

type ConfigHistoryView struct {
	ID     string            `json:"id"`
	Time   time.Time         `json:"time"`
	Values map[string]string `json:"values"`
}
type ServerConfigView struct {
	Subsystem    string              `json:"subsystem"`
	Fields       []ConfigField       `json:"fields"`
	Revision     string              `json:"revision"`
	History      []ConfigHistoryView `json:"history"`
	HistoryError string              `json:"historyError,omitempty"`
}

func ServerOperationError(err error) error {
	if err == nil {
		return nil
	}
	p := apperror.Public(err)
	if p.Code == "AdminResponseTooLarge" {
		return p
	}
	var code string
	var native madmin.ErrorResponse
	if errors.As(err, &native) {
		code = native.Code
	}
	if code == "XMinioAdminNotImplemented" || code == "NotImplemented" || code == "XMinioAdminVersionMismatch" || strings.Contains(err.Error(), "unknown subsystem:") {
		return apperror.NewWithCause(501, "ServerFeatureUnavailable", "Этот раздел недоступен в текущей конфигурации MinIO.", err)
	}
	if p.Status != 502 {
		return p
	}
	return apperror.NewWithCause(502, "ServerOperationFailed", "MinIO отклонил операцию. Обновите состояние и проверьте настройки сервера.", err)
}
func RequireServerAction(ctx context.Context, s RequireServerActionClient, actions ...string) error {
	if e := s.RefreshActionPolicy(ctx); e != nil {
		return apperror.New(403, "PermissionUnknown", "Не удалось подтвердить права на изменение сервера.")
	}
	policy := s.ActionPolicySnapshot()
	for _, a := range actions {
		if !policy.Allows(s.ActorName(), iam.Query{Action: a}) {
			return apperror.New(403, "AccessDenied", "Нет подтверждённого права на это действие.")
		}
	}
	return nil
}
func ConfigValueValid(field ConfigField, value string) bool {
	if len(value) > 80 || strings.ContainsAny(value, "\"'\n\r\t ") {
		return false
	}
	if value == "" {
		return field.Key == "drive_workers"
	}
	switch field.Kind {
	case "number":
		n, e := strconv.ParseUint(value, 10, 32)
		return e == nil && n <= 1000000
	case "duration":
		d, e := time.ParseDuration(value)
		return e == nil && d >= 0 && d <= 365*24*time.Hour
	case "select":
		for _, v := range field.Options {
			if v == value {
				return true
			}
		}
	}
	return false
}
func ConfigFields(raw, sub string) ([]ConfigField, error) {
	if len(raw) > 256*1024 {
		return nil, apperror.New(502, "ConfigTooLarge", "Ответ конфигурации превышает лимит.")
	}
	parsed, e := madmin.ParseServerConfigOutput(raw)
	if e != nil {
		return nil, apperror.New(502, "InvalidServerConfig", "Не удалось прочитать конфигурацию MinIO.")
	}
	fields := []ConfigField{}
	for _, p := range parsed {
		if p.SubSystem != sub || p.Target != "" {
			continue
		}
		for _, def := range configDefinitions(sub) {
			for _, kv := range p.KV {
				if kv.Key == def.Key {
					f := def
					f.Value = kv.Value
					f.Effective = kv.Value
					if kv.EnvOverride != nil {
						f.Env = kv.EnvOverride.Name
						f.Effective = kv.EnvOverride.Value
					}
					fields = append(fields, f)
				}
			}
		}
	}
	return fields, nil
}
func ReadServerConfig(ctx context.Context, s ReadServerConfigClient, sub string) (ServerConfigView, error) {
	v := ServerConfigView{Subsystem: sub, Fields: []ConfigField{}, History: []ConfigHistoryView{}}
	if err := validateConfigSubsystem(sub); err != nil {
		return v, err
	}
	raw, e := s.GetConfigKVWithOptions(ctx, sub, madmin.KVOptions{Env: true})
	if e != nil {
		return v, ServerOperationError(e)
	}
	v.Fields, e = ConfigFields(string(raw), sub)
	if e != nil {
		return v, e
	}
	b, _ := json.Marshal(v.Fields)
	v.Revision = revision.Of(b)
	return v, nil
}

// History contains SET inputs, potentially including credentials for other
// subsystems. Only complete, allowlisted entries may leave the API or be reapplied.
func ConfigHistory(entries []madmin.ConfigHistoryEntry, sub string) []ConfigHistoryView {
	out := []ConfigHistoryView{}
	definitions := configDefinitions(sub)
	for _, entry := range entries {
		if len(entry.Data) > 16384 {
			continue
		}
		parsed, e := madmin.ParseServerConfigOutput(entry.Data)
		if e != nil || len(parsed) != 1 || parsed[0].SubSystem != sub || parsed[0].Target != "" {
			continue
		}
		values := map[string]string{}
		safe := true
		for _, kv := range parsed[0].KV {
			found := false
			for _, f := range definitions {
				if f.Key == kv.Key && ConfigValueValid(f, kv.Value) && kv.EnvOverride == nil {
					found = true
					values[kv.Key] = kv.Value
					break
				}
			}
			if !found {
				safe = false
				break
			}
		}
		if safe && len(values) > 0 && validationsvc.ValidName(entry.RestoreID) {
			out = append(out, ConfigHistoryView{ID: entry.RestoreID, Time: entry.CreateTime, Values: values})
		}
	}
	return out
}

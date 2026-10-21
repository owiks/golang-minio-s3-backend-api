package buckets

import (
	"context"
	"fmt"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/lifecycle"

	"minio-web/api/src/services/apperror"
)

type LifecycleChangeRequest struct {
	Revision     string          `json:"revision"`
	Index        *int            `json:"index"`
	Rule         *LifecycleInput `json:"rule"`
	Enabled      *bool           `json:"enabled"`
	Confirmation string          `json:"confirmation"`
}

func ChangeLifecycle(ctx context.Context, s ChangeLifecycleClient, bucket string, operation string, body LifecycleChangeRequest) (DetailField, error) {
	state, err := ReadLifecycle(ctx, s, bucket)
	if err != nil {
		return DetailField{}, err
	}
	if body.Revision == "" || body.Revision != state.View.Revision {
		return DetailField{}, apperror.New(409, "BucketSettingChanged", "Правила Lifecycle изменились. Загрузите актуальные правила перед сохранением.")
	}
	if !state.View.Writable {
		return DetailField{}, apperror.New(409, "LifecycleUnsupported", state.View.Warning)
	}
	cfg := state.Config
	index := -1
	if operation != "create" {
		if body.Index == nil || *body.Index < 0 || *body.Index >= len(cfg.Rules) {
			return DetailField{}, apperror.New(404, "LifecycleRuleNotFound", "Правило больше не существует. Обновите список.")
		}
		index = *body.Index
	}
	switch operation {
	case "create", "update":
		if body.Rule == nil {
			return DetailField{}, apperror.New(400, "InvalidLifecycleRule", "Передайте параметры правила.")
		}
		if operation == "create" && len(cfg.Rules) >= 1000 {
			return DetailField{}, apperror.New(400, "LifecycleLimit", "В бакете допускается не более 1000 правил Lifecycle.")
		}
		old := lifecycle.Rule{}
		if index >= 0 {
			old = cfg.Rules[index]
		}
		updated, err := ApplyLifecycleInput(old, *body.Rule)
		if err != nil {
			return DetailField{}, err
		}
		for i, rule := range cfg.Rules {
			if i != index && rule.ID == updated.ID {
				return DetailField{}, apperror.New(409, "LifecycleDuplicateID", "Правило с таким именем уже существует.")
			}
		}
		if index < 0 {
			cfg.Rules = append(cfg.Rules, updated)
		} else {
			cfg.Rules[index] = updated
		}
	case "toggle":
		if body.Enabled == nil {
			return DetailField{}, apperror.New(400, "InvalidLifecycleRule", "Укажите состояние правила.")
		}
		cfg.Rules[index].Status = "Disabled"
		if *body.Enabled {
			cfg.Rules[index].Status = "Enabled"
		}
	case "delete":
		expected := cfg.Rules[index].ID
		if expected == "" {
			expected = fmt.Sprintf("Правило %d", index+1)
		}
		if body.Confirmation != expected {
			return DetailField{}, apperror.New(400, "ConfirmationRequired", "Подтвердите удаление выбранного правила.")
		}
		cfg.Rules = append(cfg.Rules[:index], cfg.Rules[index+1:]...)
	default:
		return DetailField{}, apperror.New(405, "MethodNotAllowed", "Этот способ изменения Lifecycle не поддерживается.")
	}
	status, err := s.WriteLifecycleConfig(ctx, bucket, cfg)
	if err != nil {
		switch minio.ToErrorResponse(err).Code {
		case "InvalidArgument", "InvalidRequest", "InvalidLifecycleConfiguration", "MalformedXML":
			return DetailField{}, apperror.New(400, "InvalidLifecycleRule", "MinIO отклонил правило Lifecycle. Проверьте сроки, условия и совместимость действий с настройками бакета.")
		}
		return DetailField{}, err
	}
	// The SDK's empty-config DELETE does not itself validate HTTP status.
	if status < 200 || status >= 300 {
		if status == 403 {
			return DetailField{}, apperror.New(403, "AccessDenied", "Недостаточно прав MinIO для изменения правил Lifecycle.")
		}
		return DetailField{}, apperror.New(502, "LifecycleWriteFailed", "MinIO не подтвердил изменение правил. Обновите список.")
	}
	stored, readErr := ReadLifecycle(ctx, s, bucket)
	return LifecycleField(stored, readErr), nil
}

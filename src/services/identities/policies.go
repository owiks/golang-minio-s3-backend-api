package identities

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/revision"
	"minio-web/api/src/services/validation"
)

type PolicyAdmin interface {
	ListCannedPolicies(context.Context) (map[string]json.RawMessage, error)
	InfoCannedPolicy(context.Context, string) ([]byte, error)
	AddCannedPolicy(context.Context, string, []byte) error
	RemoveCannedPolicy(context.Context, string) error
	GetPolicyEntities(context.Context, madmin.PolicyEntitiesQuery) (madmin.PolicyEntitiesResult, error)
}
type PolicyRequest struct {
	Name                       string          `json:"name,omitempty"`
	Document                   json.RawMessage `json:"document" validate:"required"`
	Revision                   string          `json:"revision,omitempty"`
	AcknowledgeNonAtomicCreate bool            `json:"acknowledgeNonAtomicCreate,omitempty"`
}
type PolicyDetails struct {
	Name     string          `json:"name"`
	Document json.RawMessage `json:"document"`
	Revision string          `json:"revision"`
}

func ListPolicies(ctx context.Context, admin PolicyAdmin) ([]string, error) {
	policies, err := admin.ListCannedPolicies(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(policies))
	for name := range policies {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
func GetPolicy(ctx context.Context, admin PolicyAdmin, name string) (PolicyDetails, error) {
	raw, err := admin.InfoCannedPolicy(ctx, name)
	if err != nil {
		return PolicyDetails{}, err
	}
	return PolicyDetails{Name: name, Document: raw, Revision: revision.Of(raw)}, nil
}

type CreatePolicyCommand struct {
	Name                       string
	Document                   json.RawMessage
	AcknowledgeNonAtomicCreate bool
}
type UpdatePolicyCommand struct {
	Document json.RawMessage
	Revision string
}

func CreatePolicy(ctx context.Context, admin PolicyAdmin, b CreatePolicyCommand) error {
	return savePolicy(ctx, admin, b.Name, PolicyRequest{Name: b.Name, Document: b.Document, AcknowledgeNonAtomicCreate: b.AcknowledgeNonAtomicCreate}, true)
}
func UpdatePolicy(ctx context.Context, admin PolicyAdmin, name string, b UpdatePolicyCommand) error {
	return savePolicy(ctx, admin, name, PolicyRequest{Document: b.Document, Revision: b.Revision}, false)
}
func savePolicy(ctx context.Context, admin PolicyAdmin, name string, b PolicyRequest, create bool) error {
	if create {
		name = b.Name
	}
	if !validation.ValidName(name) {
		return apperror.New(400, "InvalidPolicyName", "Введите корректное имя политики.")
	}
	if err := validation.ValidateDocument(b.Document); err != nil {
		return err
	}
	raw, err := admin.InfoCannedPolicy(ctx, name)
	if create {
		if err == nil {
			return apperror.New(409, "AlreadyExists", "Политика уже существует. Откройте её для редактирования.")
		}
		if !Missing(err, "XMinioAdminNoSuchPolicy") {
			return err
		}
		if !b.AcknowledgeNonAtomicCreate {
			return apperror.New(400, "PolicyCreateAcknowledgementRequired", "Подтвердите риск одновременного создания: если другой клиент займёт это имя после проверки, MinIO может перезаписать его политику, сохранив назначения.")
		}
	} else {
		if err != nil {
			return err
		}
		if b.Revision == "" || b.Revision != revision.Of(raw) {
			return apperror.New(409, "PolicyChanged", "Политика изменилась. Закройте редактор и откройте актуальную версию.")
		}
	}
	return admin.AddCannedPolicy(ctx, name, b.Document)
}
func DeletePolicy(ctx context.Context, admin PolicyAdmin, name string) error {
	entities, err := admin.GetPolicyEntities(ctx, madmin.PolicyEntitiesQuery{Policy: []string{name}})
	if err != nil {
		return err
	}
	for _, mapping := range entities.PolicyMappings {
		if len(mapping.Users)+len(mapping.Groups) > 0 {
			return apperror.New(409, "PolicyInUse", "Сначала отвяжите политику от пользователей и групп.")
		}
	}
	return admin.RemoveCannedPolicy(ctx, name)
}

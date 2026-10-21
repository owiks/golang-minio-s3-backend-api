package buckets

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/apperror"
	validationsvc "minio-web/api/src/services/validation"
)

type UpdateBucketPolicyRequest struct {
	Document json.RawMessage `json:"document"`
	Revision string          `json:"revision" validate:"required"`
}
type UpdateBucketQuotaRequest struct {
	Size     *uint64 `json:"size" validate:"required"`
	Revision string  `json:"revision" validate:"required"`
}
type UpdateBucketTagsRequest struct {
	Tags     []TagInput `json:"tags"`
	Revision string     `json:"revision" validate:"required"`
}

func PolicyChange(ctx context.Context, s PolicyChangeClient, bucket string, body UpdateBucketPolicyRequest) (DetailField, error) {
	var value PolicyDocument
	if len(body.Document) == 0 {
		return DetailField{}, apperror.New(400, "InvalidPolicy", "Передайте JSON политики или null для удаления.")
	}
	if !bytes.Equal(bytes.TrimSpace(body.Document), []byte("null")) {
		if err := validationsvc.ValidateDocument(body.Document); err != nil {
			return DetailField{}, err
		}
		var err error
		value, err = decodePolicyDocument(body.Document)
		if err != nil {
			return DetailField{}, apperror.New(400, "InvalidPolicy", "Некорректный JSON политики.")
		}
	}
	current, err := ReadPolicy(ctx, s, bucket)
	if err != nil {
		return DetailField{}, err
	}
	if err := EnsureSettingRevision(policySetting(current), body.Revision); err != nil {
		return DetailField{}, err
	}
	if err := s.SetBucketPolicy(ctx, bucket, string(value)); err != nil {
		return DetailField{}, err
	}
	// Read back MinIO's normalized value. A failed read after an acknowledged
	// write is a field error, not a retryable mutation failure.
	stored, readErr := ReadPolicy(ctx, s, bucket)
	return SettingField(policySetting(stored), readErr), nil
}
func QuotaChange(ctx context.Context, s QuotaChangeClient, bucket string, body UpdateBucketQuotaRequest) (DetailField, error) {
	if body.Size == nil || *body.Size > 9007199254740991 {
		return DetailField{}, apperror.New(400, "InvalidQuota", "Задайте целое число байт от 0 до 9 007 199 254 740 991. Ноль снимает ограничение размера.")
	}
	size := *body.Size
	current, err := ReadQuota(ctx, s, bucket)
	if err != nil {
		return DetailField{}, err
	}
	if err := EnsureSettingRevision(current, body.Revision); err != nil {
		return DetailField{}, err
	}
	current.Size = size
	current.Type = madmin.HardQuota
	if current.Size == 0 && current.Rate == 0 && current.Requests == 0 {
		current.Type = ""
	}
	// Preserve independent rate/request limits when editing only storage capacity.
	if err := s.SetBucketQuota(ctx, bucket, &madmin.BucketQuota{Size: current.Size, Type: current.Type, Rate: current.Rate, Requests: current.Requests}); err != nil {
		return DetailField{}, err
	}
	stored, readErr := ReadQuota(ctx, s, bucket)
	return SettingField(stored, readErr), nil
}
func TagsChange(ctx context.Context, s TagsChangeClient, bucket string, body UpdateBucketTagsRequest) (DetailField, error) {
	parsed, err := ValidateBucketTags(body.Tags)
	if err != nil {
		return DetailField{}, err
	}
	current, err := ReadTags(ctx, s, bucket)
	if err != nil {
		return DetailField{}, err
	}
	if err := EnsureSettingRevision(current, body.Revision); err != nil {
		return DetailField{}, err
	}
	if len(parsed.ToMap()) == 0 {
		err = s.RemoveBucketTagging(ctx, bucket)
	} else {
		err = s.SetBucketTagging(ctx, bucket, parsed)
	}
	if err != nil {
		return DetailField{}, err
	}
	stored, readErr := ReadTags(ctx, s, bucket)
	return SettingField(stored, readErr), nil
}

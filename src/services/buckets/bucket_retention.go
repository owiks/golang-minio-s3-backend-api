package buckets

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

type VersioningSetting struct {
	Status           string                 `json:"status"`
	MFADelete        string                 `json:"mfaDelete"`
	ExcludedPrefixes []minio.ExcludedPrefix `json:"excludedPrefixes"`
	ExcludeFolders   bool                   `json:"excludeFolders"`
	PurgeOnDelete    string                 `json:"purgeOnDelete"`
}
type LockingSetting struct {
	Enabled  bool                 `json:"enabled"`
	Mode     *minio.RetentionMode `json:"mode"`
	Validity *uint                `json:"validity"`
	Unit     *minio.ValidityUnit  `json:"unit"`
}
type RetentionRule struct {
	Mode     minio.RetentionMode `json:"mode"`
	Duration *uint               `json:"duration"`
	Unit     minio.ValidityUnit  `json:"unit"`
}

func ReadVersioning(ctx context.Context, s ReadVersioningClient, bucket string) (VersioningSetting, error) {
	v, err := s.GetBucketVersioning(ctx, bucket)
	return VersioningSetting{Status: v.Status, MFADelete: v.MFADelete, ExcludedPrefixes: v.ExcludedPrefixes, ExcludeFolders: v.ExcludeFolders, PurgeOnDelete: v.PurgeOnDelete}, err
}
func ReadLocking(ctx context.Context, s ReadLockingClient, bucket string) (LockingSetting, error) {
	enabled, mode, validity, unit, err := s.GetObjectLockConfig(ctx, bucket)
	if minio.ToErrorResponse(err).Code == "ObjectLockConfigurationNotFoundError" {
		err = nil
	}
	return LockingSetting{Enabled: enabled == "Enabled", Mode: mode, Validity: validity, Unit: unit}, err
}
func ParseRetention(raw json.RawMessage) (*RetentionRule, error) {
	if len(raw) == 0 {
		return nil, apperror.New(400, "InvalidRetention", messages.BucketsInvalidRetention)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var rule RetentionRule
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&rule) != nil || !rule.Mode.IsValid() || rule.Duration == nil || *rule.Duration == 0 || (rule.Unit != minio.Days && rule.Unit != minio.Years) {
		return nil, apperror.New(400, "InvalidRetention", messages.BucketsInvalidRetentionBucketRetention)
	}
	if (rule.Unit == minio.Days && *rule.Duration > 36500) || (rule.Unit == minio.Years && *rule.Duration > 100) {
		return nil, apperror.New(400, "InvalidRetention", messages.BucketsInvalidRetentionBucketRetentionParseRetention)
	}
	return &rule, nil
}

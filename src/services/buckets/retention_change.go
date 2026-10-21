package buckets

import (
	"context"
	"encoding/json"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
)

type UpdateBucketVersioningRequest struct {
	Status   string `json:"status"`
	Revision string `json:"revision" validate:"required"`
}
type UpdateBucketLockingRequest struct {
	Enabled          *bool           `json:"enabled"`
	DefaultRetention json.RawMessage `json:"defaultRetention"`
	Revision         string          `json:"revision" validate:"required"`
}

func ChangeRetention(ctx context.Context, s ChangeRetentionClient, bucket, kind string, versioning UpdateBucketVersioningRequest, locking UpdateBucketLockingRequest) error {
	var expected, status string
	var rule *RetentionRule
	if kind == "versioning" {
		b := versioning
		if b.Status != minio.Enabled && b.Status != minio.Suspended {
			return apperror.New(400, "InvalidVersioning", "Версионирование можно включить или приостановить. Возврат в состояние «не включено» не поддерживается.")
		}
		expected, status = b.Revision, b.Status
	} else {
		b := locking
		if b.Enabled == nil || !*b.Enabled {
			return apperror.New(400, "InvalidObjectLock", "Object Lock можно включить, но нельзя отключить. Для удаления срока по умолчанию передайте defaultRetention: null и enabled: true.")
		}
		parsed, err := ParseRetention(b.DefaultRetention)
		if err != nil {
			return err
		}
		rule = parsed
		expected = b.Revision
	}
	current, err := ReadBucketSetting(ctx, s, bucket, kind)
	if err != nil {
		return err
	}
	if err := EnsureSettingRevision(current, expected); err != nil {
		return err
	}
	if kind == "versioning" {
		v := current.(VersioningSetting)
		// Preserve exclusions, MFA and purge settings; only the selected status changes.
		err = s.SetBucketVersioning(ctx, bucket, minio.BucketVersioningConfiguration{Status: status, MFADelete: v.MFADelete, ExcludedPrefixes: v.ExcludedPrefixes, ExcludeFolders: v.ExcludeFolders, PurgeOnDelete: v.PurgeOnDelete})
		if status == minio.Suspended && minio.ToErrorResponse(err).Code == "InvalidBucketState" {
			return apperror.New(409, "VersioningRestricted", "Сервер не разрешает приостановить версионирование этого бакета. При включённом Object Lock оно должно оставаться включённым.")
		}
	} else {
		lock := current.(LockingSetting)
		if !lock.Enabled {
			v, e := ReadVersioning(ctx, s, bucket)
			if e != nil {
				return e
			}
			if v.Status != minio.Enabled {
				return apperror.New(409, "VersioningRequired", "Сначала включите версионирование бакета, затем включите Object Lock.")
			}
		}
		if rule == nil {
			err = s.SetBucketObjectLockConfig(ctx, bucket, nil, nil, nil)
		} else {
			err = s.SetBucketObjectLockConfig(ctx, bucket, &rule.Mode, rule.Duration, &rule.Unit)
		}
		if !lock.Enabled && minio.ToErrorResponse(err).Code == "InvalidBucketState" {
			return apperror.New(409, "ObjectLockUnavailable", "Сервер не разрешил включить Object Lock для существующего бакета. Если он поддерживает эту настройку только при создании, создайте бакет с Object Lock.")
		}
	}
	if err != nil {
		return err
	}
	return nil
}

package files

import (
	"context"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	bucketssvc "minio-web/api/src/services/buckets"
	objectssvc "minio-web/api/src/services/objects"
	"minio-web/api/src/services/result"
)

func GetVersions(ctx context.Context, s GetVersionsClient, bucket, key string, cursor string) (objectssvc.VersionsPage, error) {
	bucket, key, err := objectssvc.ObjectParams(bucket, key)
	if err != nil {
		return objectssvc.VersionsPage{}, err
	}
	page, err := objectssvc.ListVersions(ctx, s, bucket, key, cursor)
	if err != nil {
		return objectssvc.VersionsPage{}, err
	}
	return page, nil
}

func GetVersionLink(ctx context.Context, s GetObjectLinkClient, bucket, key string, id string) (result.Link, error) {
	bucket, key, err := objectssvc.ObjectParams(bucket, key)
	if err != nil {
		return result.Link{}, err
	}

	if id == "" || len(id) > 1024 || strings.ContainsFunc(id, unicode.IsControl) {
		return result.Link{}, apperror.New(400, "InvalidVersion", "Выберите конкретную версию файла.")
	}
	q := url.Values{"versionId": {id}, "response-content-type": {"application/octet-stream"}, "response-content-disposition": {`attachment; filename="download"; filename*=UTF-8''` + url.PathEscape(path.Base(key))}}
	u, err := s.PresignedGetObject(ctx, bucket, key, time.Minute, q)
	if err != nil {
		return result.Link{}, err
	}
	return result.Link{URL: u.String()}, nil
}

func GetVersionProtection(ctx context.Context, s GetVersionProtectionClient, bucket, key string, id string) (VersionProtection, error) {
	bucket, key, err := objectssvc.ObjectParams(bucket, key)
	if err != nil {
		return VersionProtection{}, err
	}

	if id == "" || len(id) > 1024 || strings.ContainsFunc(id, unicode.IsControl) {
		return VersionProtection{}, apperror.New(400, "InvalidVersion", "Выберите конкретную версию файла.")
	}
	result := VersionProtection{}
	for _, kind := range []string{"retention", "hold"} {
		v, err := objectssvc.ReadObjectProtection(ctx, s, bucket, key, id, kind)
		if kind == "retention" {
			result.Retention = bucketssvc.SettingField(v, err)
		} else {
			result.Hold = bucketssvc.SettingField(v, err)
		}
	}
	return result, nil
}

type ProtectionKind string

const (
	RetentionProtection ProtectionKind = "retention"
	LegalHoldProtection ProtectionKind = "hold"
)

func UpdateVersionProtection(ctx context.Context, s UpdateVersionProtectionClient, bucket, key string, id string, kind ProtectionKind, b ObjectVersionsRequest) (bucketssvc.DetailField, error) {
	bucket, key, err := objectssvc.ObjectParams(bucket, key)
	if err != nil {
		return bucketssvc.DetailField{}, err
	}

	if id == "" || len(id) > 1024 || strings.ContainsFunc(id, unicode.IsControl) {
		return bucketssvc.DetailField{}, apperror.New(400, "InvalidVersion", "Выберите конкретную версию файла.")
	}
	if kind != RetentionProtection && kind != LegalHoldProtection {
		return bucketssvc.DetailField{}, apperror.New(400, "InvalidProtection", "Выберите вид защиты.")
	}
	v, err := objectssvc.ReadObjectProtection(ctx, s, bucket, key, id, string(kind))
	if err != nil {
		return bucketssvc.DetailField{}, err
	}
	if err := bucketssvc.EnsureSettingRevision(v, b.Revision); err != nil {
		return bucketssvc.DetailField{}, err
	}
	if b.Confirmation != id {
		return bucketssvc.DetailField{}, apperror.New(400, "ConfirmationRequired", "Подтвердите изменение выбранной версии.")
	}
	if kind == LegalHoldProtection {
		status := minio.LegalHoldStatus(b.Status)
		if !status.IsValid() {
			return bucketssvc.DetailField{}, apperror.New(400, "InvalidHold", "Выберите ON или OFF.")
		}
		err = s.PutObjectLegalHold(ctx, bucket, key, minio.PutObjectLegalHoldOptions{VersionID: id, Status: &status})
	} else {
		opts := minio.PutObjectRetentionOptions{VersionID: id}
		if b.Mode != "" {
			mode := minio.RetentionMode(b.Mode)
			date, e := time.Parse(time.RFC3339, b.Until)
			if !mode.IsValid() || e != nil || !date.After(time.Now()) {
				return bucketssvc.DetailField{}, apperror.New(400, "InvalidRetention", "Выберите режим и дату окончания в будущем.")
			}
			opts.Mode = &mode
			opts.RetainUntilDate = &date
		}
		err = s.PutObjectRetention(ctx, bucket, key, opts)
	}
	if err != nil {
		if code := minio.ToErrorResponse(err).Code; code == "AccessDenied" || code == "InvalidRequest" || code == "ObjectLocked" {
			return bucketssvc.DetailField{}, apperror.New(403, "ObjectProtected", "MinIO отклонил изменение: проверьте права и действующий срок защиты. Обход GOVERNANCE не используется; срок COMPLIANCE нельзя сократить.")
		}
		return bucketssvc.DetailField{}, err
	}
	current, err := objectssvc.ReadObjectProtection(ctx, s, bucket, key, id, string(kind))
	return bucketssvc.SettingField(current, err), nil
}

type ObjectVersionsRequest struct {
	Revision     string `json:"revision"`
	Mode         string `json:"mode"`
	Until        string `json:"until"`
	Status       string `json:"status"`
	Confirmation string `json:"confirmation"`
}

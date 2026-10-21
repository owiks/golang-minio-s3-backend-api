package objects

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/storage"
)

type ObjectVersion struct {
	ID       string    `json:"id" xml:"VersionId"`
	Key      string    `json:"-" xml:"Key"`
	Modified time.Time `json:"modified" xml:"LastModified"`
	Size     int64     `json:"size" xml:"Size"`
	Latest   bool      `json:"latest" xml:"IsLatest"`
	Deleted  bool      `json:"deleted" xml:"-"`
}
type VersionsPage struct {
	Rows []ObjectVersion `json:"rows"`
	Next string          `json:"next"`
}

// The adapter preserves native version markers. This service binds the opaque
// UI cursor to one object and excludes other keys sharing the same prefix.
func ListVersions(ctx context.Context, s ListVersionsClient, bucket, key, cursor string) (VersionsPage, error) {
	page := VersionsPage{Rows: []ObjectVersion{}}
	keyMarker, versionMarker := "", ""
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || len(raw) > 8192 {
			return page, apperror.New(400, "InvalidCursor", "Некорректный курсор версий.")
		}
		var c struct{ Bucket, Key, Version, Marker string }
		if json.Unmarshal(raw, &c) != nil || c.Bucket != bucket || c.Key != key || !validVersionMarker(c.Version, 1024) || !validVersionMarker(c.Marker, 4096) {
			return page, apperror.New(400, "InvalidCursor", "Курсор относится к другому файлу.")
		}
		keyMarker, versionMarker = c.Marker, c.Version
	}
	listed, err := s.ReadVersionPage(ctx, bucket, key, keyMarker, versionMarker, 50)
	if errors.Is(err, storage.ErrInvalidVersionResponse) {
		return page, apperror.New(502, "VersionListingFailed", "MinIO не вернул список версий.")
	}
	if err != nil {
		return page, err
	}
	invalid := apperror.New(502, "VersionListingFailed", "MinIO вернул некорректное продолжение списка версий. Обновите список.")

	for i := range listed.Rows {
		decoded, e := url.QueryUnescape(listed.Rows[i].Key)
		if e != nil {
			return page, invalid
		}
		listed.Rows[i].Key = decoded
	}
	listed.KeyMarker, err = url.QueryUnescape(listed.KeyMarker)
	if err != nil {
		return page, invalid
	}
	if listed.Truncated {
		if len(listed.Rows) == 0 || !validVersionMarker(listed.KeyMarker, 4096) {
			return page, invalid
		}
		// A later key exhausts this object's versions. If the last row still
		// belongs to this object, both markers must describe a usable new page.
		// MinIO may attach cache metadata to NextKeyMarker: preserve the entire
		// opaque marker rather than comparing it to the object's name.
		if listed.Rows[len(listed.Rows)-1].Key == key {
			if !validVersionMarker(listed.VersionMarker, 1024) || listed.KeyMarker == keyMarker && listed.VersionMarker == versionMarker {
				return page, invalid
			}
		}
	}
	for _, row := range listed.Rows {
		if row.Key == key {
			page.Rows = append(page.Rows, ObjectVersion{ID: row.ID, Key: row.Key, Modified: row.Modified, Size: row.Size, Latest: row.Latest, Deleted: row.Deleted})
		}
	}
	if listed.Truncated && listed.Rows[len(listed.Rows)-1].Key == key {
		raw, _ := json.Marshal(struct{ Bucket, Key, Version, Marker string }{bucket, key, listed.VersionMarker, listed.KeyMarker})
		page.Next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}

func validVersionMarker(value string, limit int) bool {
	return value != "" && len(value) <= limit && !strings.ContainsFunc(value, unicode.IsControl)
}

type ObjectRetention struct {
	Mode  string     `json:"mode"`
	Until *time.Time `json:"until"`
}

func ReadObjectProtection(ctx context.Context, s ReadObjectProtectionClient, bucket, key, id, kind string) (ProtectionValue, error) {
	if kind == "hold" {
		v, err := s.GetObjectLegalHold(ctx, bucket, key, minio.GetObjectLegalHoldOptions{VersionID: id})
		if minio.ToErrorResponse(err).Code == "NoSuchObjectLockConfiguration" {
			return LegalHoldValue("OFF"), nil
		}
		if err != nil {
			return nil, err
		}
		if v == nil {
			return LegalHoldValue("OFF"), nil
		}
		return LegalHoldValue(*v), nil
	}
	mode, until, err := s.GetObjectRetention(ctx, bucket, key, id)
	if minio.ToErrorResponse(err).Code == "NoSuchObjectLockConfiguration" {
		return ObjectRetention{}, nil
	}
	if err != nil {
		return nil, err
	}
	value := ObjectRetention{Until: until}
	if mode != nil {
		value.Mode = string(*mode)
	}
	return value, nil
}

// ProtectionValue distinguishes the two native wire values without accepting
// unrelated SDK objects or exposing their internal configuration.
type ProtectionValue interface{ objectProtection() }
type LegalHoldValue string

func (LegalHoldValue) objectProtection()  {}
func (ObjectRetention) objectProtection() {}

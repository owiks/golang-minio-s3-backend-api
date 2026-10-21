package files

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"minio-web/api/src/services/apperror"
	objectssvc "minio-web/api/src/services/objects"
	searchsvc "minio-web/api/src/services/search"
	"minio-web/api/src/services/storage"
)

type ObjectUsageOptions struct {
	Bucket, Prefix, Token string
	Versions              bool
}

func ReadObjectUsage(ctx context.Context, s ReadObjectUsageClient, input ObjectUsageOptions) (ObjectUsage, error) {
	bucket, prefix, token := input.Bucket, input.Prefix, input.Token
	if bucket == "" || len(bucket) > 63 || len(prefix) > 1024 || len(token) > 16384 {
		return ObjectUsage{}, apperror.New(400, "InvalidUsageScope", "Откройте корректный бакет и папку.")
	}
	rows := []objectssvc.ObjectUsageRow{}
	next := ""
	scanned := 0
	if !input.Versions {
		page, err := searchsvc.RecursivePage(ctx, s, bucket, prefix, token)
		if err != nil {
			return ObjectUsage{}, err
		}
		scanned = len(page.Contents)
		for _, obj := range page.Contents {
			if strings.HasSuffix(obj.Key, "/") && obj.Size == 0 {
				continue
			}
			rows = append(rows, objectssvc.ObjectUsageRow{Key: obj.Key, Size: obj.Size, Modified: obj.LastModified, Kind: "current"})
		}
		if page.IsTruncated {
			next = page.NextContinuationToken
		}
	} else {
		keyMarker, versionMarker := "", ""
		if token != "" {
			var cursor objectssvc.UsageCursor
			raw, e := base64.RawURLEncoding.DecodeString(token)
			if e != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Bucket != bucket || cursor.Prefix != prefix || cursor.Marker == "" || !strings.HasPrefix(cursor.Marker, prefix) {
				return ObjectUsage{}, apperror.New(400, "InvalidUsageCursor", "Курсор относится к другому расчёту.")
			}
			keyMarker, versionMarker = cursor.Marker, cursor.Version
		}
		page, err := s.ReadVersionPage(ctx, bucket, prefix, keyMarker, versionMarker, 500)
		if errors.Is(err, storage.ErrInvalidVersionResponse) {
			return ObjectUsage{}, apperror.New(502, "UsageVersionsUnavailable", "Хранилище не вернуло список версий.")
		}
		if err != nil {
			return ObjectUsage{}, err
		}
		if len(page.Rows) > 500 {
			return ObjectUsage{}, apperror.New(502, "InvalidUsagePage", "Хранилище вернуло слишком много записей.")
		}
		scanned = len(page.Rows)
		for _, obj := range page.Rows {
			key, e := url.QueryUnescape(obj.Key)
			if e != nil || !strings.HasPrefix(key, prefix) {
				return ObjectUsage{}, apperror.New(502, "InvalidUsagePage", "Получены записи вне выбранной папки.")
			}
			kind := "version"
			if obj.Deleted {
				kind = "deleted"
			} else if obj.Latest {
				kind = "current"
			}
			if strings.HasSuffix(key, "/") && obj.Size == 0 && !obj.Deleted {
				continue
			}
			rows = append(rows, objectssvc.ObjectUsageRow{Key: key, Size: obj.Size, Modified: obj.Modified, Kind: kind})
		}
		if page.Truncated {
			marker, e := url.QueryUnescape(page.KeyMarker)
			if e != nil || marker == "" {
				return ObjectUsage{}, apperror.New(502, "InvalidUsagePage", "Хранилище не вернуло продолжение списка.")
			}
			raw, _ := json.Marshal(objectssvc.UsageCursor{Bucket: bucket, Prefix: prefix, Marker: marker, Version: page.VersionMarker})
			next = base64.RawURLEncoding.EncodeToString(raw)
		}
	}
	return ObjectUsage{Rows: rows, Scanned: scanned, NextToken: next}, nil
}

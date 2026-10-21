package objects

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

const MaxObjectAttributeParts = 1000

type ObjectAttributesInput struct {
	Bucket           string
	Key              string
	VersionID        string
	MaxParts         int
	PartNumberMarker int
	Snapshot         string
}

type ObjectAttributes struct {
	storage.ObjectAttributesPage
	// version is an immutable S3 version; observed compares response identity
	// between pages but cannot turn an unversioned/null slot into a snapshot.
	Consistency string `json:"consistency"`
	Snapshot    string `json:"snapshot,omitempty"`
}

func GetAttributes(ctx context.Context, client ReadAttributesClient, input ObjectAttributesInput) (ObjectAttributes, error) {
	bucket, key, err := ObjectParams(input.Bucket, input.Key)
	if err != nil {
		return ObjectAttributes{}, err
	}
	if len(input.VersionID) > 1024 || strings.ContainsFunc(input.VersionID, unicode.IsControl) {
		return ObjectAttributes{}, apperror.New(400, "InvalidVersion", messages.ObjectsInvalidVersion)
	}
	if input.MaxParts == 0 {
		input.MaxParts = 100
	}
	if input.MaxParts < 1 || input.MaxParts > MaxObjectAttributeParts || input.PartNumberMarker < 0 || input.PartNumberMarker > 10000 {
		return ObjectAttributes{}, apperror.New(400, "InvalidObjectAttributes", messages.ObjectsInvalidObjectAttributes)
	}
	if input.Snapshot != "" {
		if raw, e := hex.DecodeString(input.Snapshot); e != nil || len(raw) != sha256.Size {
			return ObjectAttributes{}, apperror.New(400, "InvalidAttributesSnapshot", messages.ObjectsInvalidAttributesSnapshot)
		}
	}
	if input.PartNumberMarker > 0 && !immutablePropertyVersion(input.VersionID) && input.Snapshot == "" {
		return ObjectAttributes{}, apperror.New(400, "AttributesSnapshotRequired", messages.ObjectsAttributesSnapshotRequired)
	}
	page, err := client.ReadObjectAttributes(ctx, bucket, key, storage.ObjectAttributesOptions{VersionID: input.VersionID, MaxParts: input.MaxParts, PartNumberMarker: input.PartNumberMarker})
	if err != nil {
		return ObjectAttributes{}, err
	}
	result := ObjectAttributes{ObjectAttributesPage: page, Consistency: "observed"}
	if immutablePropertyVersion(page.VersionID) {
		result.Consistency = "version"
	}
	if page.ETag != "" && page.Size != nil && !page.LastModified.IsZero() {
		identity := struct {
			Bucket, Key, VersionID, ETag, StorageClass string
			Size                                       int64
			Modified                                   string
			PartsCount                                 *int
			Checksums                                  map[string]string
			ChecksumType                               string
		}{bucket, key, page.VersionID, page.ETag, page.StorageClass, *page.Size, page.LastModified.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), page.PartsCount, page.Checksums, page.ChecksumType}
		raw, _ := json.Marshal(identity)
		digest := sha256.Sum256(raw)
		result.Snapshot = hex.EncodeToString(digest[:])
	}
	if input.Snapshot != "" && input.Snapshot != result.Snapshot {
		return ObjectAttributes{}, apperror.New(409, "ObjectAttributesChanged", messages.ObjectsObjectAttributesChanged)
	}
	if page.Truncated != nil && *page.Truncated && result.Snapshot == "" && !immutablePropertyVersion(input.VersionID) {
		return ObjectAttributes{}, apperror.New(502, "AttributesSnapshotUnavailable", messages.ObjectsAttributesSnapshotUnavailable)
	}
	return result, nil
}

package batch

import (
	"encoding/json"
	"strings"
	"time"
	"unicode"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/s3utils"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

type BatchInput struct {
	Type         string `json:"type"`
	Bucket       string `json:"bucket"`
	Prefix       string `json:"prefix"`
	OlderHours   int    `json:"olderHours"`
	KeepVersions int    `json:"keepVersions"`
	Encryption   string `json:"encryption"`
	KeyID        string `json:"keyId"`
	Confirmation string `json:"confirmation"`
}

func BatchDocument(b BatchInput) (string, error) {
	invalid := func() (string, error) {
		return "", apperror.New(400, "InvalidBatch", messages.BatchInvalidBatch)
	}
	if s3utils.CheckValidBucketNameStrict(b.Bucket) != nil || len(b.Prefix) > 1024 || strings.ContainsFunc(b.Prefix, unicode.IsControl) || strings.ContainsAny(b.Prefix, "*?[") {
		return invalid()
	}
	if b.Confirmation != b.Bucket {
		return "", apperror.New(400, "ConfirmationRequired", messages.BatchConfirmationRequired)
	}
	job := map[string]any{"apiVersion": "v1", "bucket": b.Bucket, "prefix": b.Prefix}
	switch b.Type {
	case "expire":
		if b.OlderHours < 1 || b.OlderHours > 876000 || b.KeepVersions < 0 || b.KeepVersions > 10000 {
			return invalid()
		}
		job["rules"] = []any{map[string]any{"type": "object", "name": "*", "olderThan": (time.Duration(b.OlderHours) * time.Hour).String(), "purge": map[string]int{"retainVersions": b.KeepVersions}}}
	case "keyrotate":
		if (b.Encryption != "sse-s3" && b.Encryption != "sse-kms") || len(b.KeyID) > 1024 || strings.ContainsFunc(b.KeyID, unicode.IsControl) || b.Encryption == "sse-s3" && b.KeyID != "" {
			return invalid()
		}
		job["encryption"] = map[string]string{"type": b.Encryption, "key": b.KeyID}
	default:
		return invalid()
	}
	// JSON is a YAML subset accepted by MinIO's native job parser. The server
	// constructs the complete document: no arbitrary notification URLs or secrets.
	raw, _ := json.Marshal(map[string]any{b.Type: job})
	return string(raw), nil
}

func SafeBatchMetric(m madmin.JobMetric) BatchMetric {
	v := BatchMetric{JobID: m.JobID, JobType: m.JobType, StartTime: m.StartTime, LastUpdate: m.LastUpdate, RetryAttempts: m.RetryAttempts, Complete: m.Complete, Failed: m.Failed}
	if c := m.Replicate; c != nil {
		v.Replicate = &ReplicationMetric{ExpirationMetric: ExpirationMetric{RotationMetric: RotationMetric{LastBucket: c.Bucket, LastObject: c.Object, Objects: c.Objects, ObjectsFailed: c.ObjectsFailed}, DeleteMarkers: c.DeleteMarkers, DeleteMarkersFailed: c.DeleteMarkersFailed}, BytesTransferred: c.BytesTransferred, BytesFailed: c.BytesFailed}
	}
	if c := m.KeyRotate; c != nil {
		v.Rotation = &RotationMetric{LastBucket: c.Bucket, LastObject: c.Object, Objects: c.Objects, ObjectsFailed: c.ObjectsFailed}
	}
	if c := m.Expired; c != nil {
		v.Expired = &ExpirationMetric{RotationMetric: RotationMetric{LastBucket: c.Bucket, LastObject: c.Object, Objects: c.Objects, ObjectsFailed: c.ObjectsFailed}, DeleteMarkers: c.DeleteMarkers, DeleteMarkersFailed: c.DeleteMarkersFailed}
	}
	if c := m.Catalog; c != nil {
		v.Catalog = &CatalogMetric{ObjectsScannedCount: c.ObjectsScannedCount, ObjectsMatchedCount: c.ObjectsMatchedCount, RecordsWrittenCount: c.RecordsWrittenCount, OutputObjectsCount: c.OutputObjectsCount}
	}
	return v
}

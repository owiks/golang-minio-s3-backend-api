package batch

import "time"

// Only UI counters are exported; SDK job documents and upstream errors stay private.
type BatchMetric struct {
	JobID         string             `json:"jobID"`
	JobType       string             `json:"jobType"`
	StartTime     time.Time          `json:"startTime"`
	LastUpdate    time.Time          `json:"lastUpdate"`
	RetryAttempts int                `json:"retryAttempts"`
	Complete      bool               `json:"complete"`
	Failed        bool               `json:"failed"`
	Replicate     *ReplicationMetric `json:"replicate,omitempty"`
	Rotation      *RotationMetric    `json:"rotation,omitempty"`
	Expired       *ExpirationMetric  `json:"expired,omitempty"`
	Catalog       *CatalogMetric     `json:"catalog,omitempty"`
}
type RotationMetric struct {
	LastBucket    string `json:"lastBucket"`
	LastObject    string `json:"lastObject"`
	Objects       int64  `json:"objects"`
	ObjectsFailed int64  `json:"objectsFailed"`
}
type ExpirationMetric struct {
	RotationMetric
	DeleteMarkers       int64 `json:"deleteMarkers"`
	DeleteMarkersFailed int64 `json:"deleteMarkersFailed"`
}
type ReplicationMetric struct {
	ExpirationMetric
	BytesTransferred int64 `json:"bytesTransferred"`
	BytesFailed      int64 `json:"bytesFailed"`
}
type CatalogMetric struct {
	ObjectsScannedCount uint64 `json:"objectsScannedCount"`
	ObjectsMatchedCount uint64 `json:"objectsMatchedCount"`
	RecordsWrittenCount uint64 `json:"recordsWrittenCount"`
	OutputObjectsCount  uint64 `json:"outputObjectsCount"`
}

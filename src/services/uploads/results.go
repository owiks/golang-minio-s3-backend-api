package uploads

import (
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads/integrity"
)

type InviteInfo struct {
	Bucket    string    `json:"bucket"`
	Prefix    string    `json:"prefix"`
	ExpiresAt time.Time `json:"expiresAt"`
	MaxSize   int64     `json:"maxSize"`
	MaxFiles  int       `json:"maxFiles"`
	Uploaded  int       `json:"uploaded"`
}
type IncompleteUploadState struct {
	State string       `json:"state"`
	Parts *UploadParts `json:"parts,omitempty"`
}
type AbortUploadResult struct {
	State   string       `json:"state"`
	OK      *bool        `json:"ok,omitempty"`
	Message messages.Ref `json:"message,omitempty"`
}
type IncompleteUploadRow struct {
	Key          string       `json:"key"`
	ID           string       `json:"uploadId"`
	Initiated    time.Time    `json:"initiated"`
	Owner        string       `json:"owner"`
	Parts        *UploadParts `json:"parts"`
	Error        messages.Ref `json:"error,omitempty"`
	Initiator    string       `json:"initiator,omitempty"`
	StorageClass string       `json:"storageClass,omitempty"`
}
type IncompleteUploadsPage struct {
	Items          []IncompleteUploadRow `json:"items"`
	HasMore        bool                  `json:"hasMore"`
	KeyMarker      string                `json:"keyMarker"`
	UploadIDMarker string                `json:"uploadIdMarker"`
}

type ImportResult struct {
	OK   bool  `json:"ok"`
	Size int64 `json:"size"`
}

type UploadReceipt struct {
	OK        bool                `json:"ok"`
	ETag      string              `json:"etag"`
	VersionID string              `json:"versionId"`
	Size      int64               `json:"size"`
	Checksum  *integrity.Evidence `json:"checksum,omitempty"`
	// A mismatch reported after a successful write is represented in the
	// receipt, not as a retryable write error. No readback or rollback is issued.
	WriteState string `json:"writeState,omitempty"`
}

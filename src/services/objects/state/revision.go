package objectstate

import (
	"encoding/json"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/revision"
)

type DeletionSnapshot struct {
	ETag     string    `json:"etag"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Version  string    `json:"version"`
}

func DeletionRevision(v DeletionSnapshot) string { raw, _ := json.Marshal(v); return revision.Of(raw) }

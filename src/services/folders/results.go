package folders

import (
	"time"

	objectssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/result"
)

type CreatedFolder struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
}
type FolderDeletionInfo struct {
	Items   []FolderDeleteItem `json:"items"`
	Files   int                `json:"files"`
	Markers int                `json:"markers"`
	Size    int64              `json:"size"`
}
type FolderObjectInfo struct {
	Revision string `json:"revision"`
	Size     int64  `json:"size"`
}
type PublicFolderInfo struct {
	Bucket    string    `json:"bucket"`
	Prefix    string    `json:"prefix"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// PublicFolderResult is an internal union. The HTTP controller emits the selected
// JSON value or binary preview; this container itself is never serialized.
type PublicFolderResult struct {
	Info    *PublicFolderInfo       `json:"-"`
	Page    *objectssvc.ObjectPage  `json:"-"`
	Link    *result.Link            `json:"-"`
	Preview *objectssvc.PreviewData `json:"-"`
}

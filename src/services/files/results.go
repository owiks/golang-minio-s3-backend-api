package files

import (
	"time"

	bucketssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/buckets"
	objectssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
	searchsvc "github.com/owiks/golang-minio-s3-backend-api/src/services/search"
)

type ObjectInfo struct {
	Size        int64             `json:"size"`
	ContentType string            `json:"contentType"`
	Modified    time.Time         `json:"modified"`
	ETag        string            `json:"etag"`
	VersionID   string            `json:"versionId"`
	Metadata    map[string]string `json:"metadata"`
}
type ObjectLink struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type ObjectUsage struct {
	Rows      []objectssvc.ObjectUsageRow `json:"rows"`
	Scanned   int                         `json:"scanned"`
	NextToken string                      `json:"nextToken"`
}
type ContentSearchResult struct {
	Matches   []searchsvc.ContentMatch `json:"matches"`
	Issues    []searchsvc.ContentIssue `json:"issues"`
	Scanned   int                      `json:"scanned"`
	Read      int                      `json:"read"`
	Skipped   int                      `json:"skipped"`
	NextToken string                   `json:"nextToken"`
}

type VersionProtection struct {
	Retention bucketssvc.DetailField `json:"retention"`
	Hold      bucketssvc.DetailField `json:"hold"`
}

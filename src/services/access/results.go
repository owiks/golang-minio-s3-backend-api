package access

import (
	"minio-web/api/src/services/iam"
	"minio-web/api/src/services/objects"
)

type AccessSnapshot struct {
	Buckets  []objects.BucketRow `json:"buckets"`
	Access   NavigationAccess    `json:"access"`
	Revision string              `json:"revision"`
}
type ActionAccess struct {
	Allowed []bool                `json:"allowed"`
	Known   bool                  `json:"known"`
	Issue   *iam.ActionCheckIssue `json:"issue"`
}
type AccessRevision struct {
	Revision string `json:"revision"`
}

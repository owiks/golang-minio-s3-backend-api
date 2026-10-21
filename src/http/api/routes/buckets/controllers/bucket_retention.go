package controllers

import (
	"net/http"

	httpvalidation "github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	bucketssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/buckets"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func HandleUpdateBucketRetention(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, bucket, kind string) error {
	var versioning bucketssvc.UpdateBucketVersioningRequest
	var locking bucketssvc.UpdateBucketLockingRequest
	if kind == "versioning" {
		if err := httpvalidation.Decode(w, r, &versioning); err != nil {
			return err
		}
	} else {
		if err := httpvalidation.Decode(w, r, &locking); err != nil {
			return err
		}
	}
	if err := bucketssvc.ChangeRetention(r.Context(), runtimesvc.NewClients(s), bucket, kind, versioning, locking); err != nil {
		return err
	}
	return HandleBucketDetails(app, w, r, s, bucket)
}

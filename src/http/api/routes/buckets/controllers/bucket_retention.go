package controllers

import (
	"net/http"

	httpvalidation "minio-web/api/src/http/validation"
	bucketssvc "minio-web/api/src/services/buckets"
	runtimesvc "minio-web/api/src/services/runtime"
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

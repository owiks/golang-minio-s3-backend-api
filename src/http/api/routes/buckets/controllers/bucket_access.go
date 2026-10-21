package controllers

import (
	"net/http"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	bucketssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/buckets"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func HandleBucketAccess(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, bucket string) error {
	value, err := bucketssvc.ReadBucketAccess(r.Context(), runtimesvc.NewClients(s), bucket)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

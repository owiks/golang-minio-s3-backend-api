package controllers

import (
	"net/http"

	httpcommon "minio-web/api/src/http/api/common"
	bucketssvc "minio-web/api/src/services/buckets"
	runtimesvc "minio-web/api/src/services/runtime"
)

func HandleBucketAccess(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, bucket string) error {
	value, err := bucketssvc.ReadBucketAccess(r.Context(), runtimesvc.NewClients(s), bucket)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}

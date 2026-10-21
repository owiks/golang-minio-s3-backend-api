package controllers

import (
	"net/http"

	httpcommon "minio-web/api/src/http/api/common"
	bucketssvc "minio-web/api/src/services/buckets"
	runtimesvc "minio-web/api/src/services/runtime"
)

func HandleBucketUsage(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	value, err := bucketssvc.ReadUsage(r.Context(), runtimesvc.NewClients(s))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}
func HandleBucketDetails(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, bucket string) error {
	value, err := bucketssvc.ReadDetails(r.Context(), runtimesvc.NewClients(s), bucket, r.URL.Query().Get("fields"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}

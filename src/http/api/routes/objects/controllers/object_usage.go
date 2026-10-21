package controllers

import (
	"net/http"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	objectssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/files"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func HandleObjectUsage(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "GET" {
		return apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
	}
	q := r.URL.Query()
	value, err := objectssvc.ReadObjectUsage(r.Context(), runtimesvc.NewClients(s), objectssvc.ObjectUsageOptions{Bucket: q.Get("bucket"), Prefix: q.Get("prefix"), Token: q.Get("token"), Versions: q.Get("versions") == "1"})
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

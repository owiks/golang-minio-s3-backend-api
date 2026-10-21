package controllers

import (
	"net/http"

	httpcommon "minio-web/api/src/http/api/common"
	"minio-web/api/src/services/apperror"
	objectssvc "minio-web/api/src/services/files"
	runtimesvc "minio-web/api/src/services/runtime"
)

func HandleObjectUsage(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "GET" {
		return apperror.New(405, "MethodNotAllowed", "Метод не поддерживается.")
	}
	q := r.URL.Query()
	value, err := objectssvc.ReadObjectUsage(r.Context(), runtimesvc.NewClients(s), objectssvc.ObjectUsageOptions{Bucket: q.Get("bucket"), Prefix: q.Get("prefix"), Token: q.Get("token"), Versions: q.Get("versions") == "1"})
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}

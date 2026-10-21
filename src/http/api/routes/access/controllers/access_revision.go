package controllers

import (
	"net/http"

	httpcommon "minio-web/api/src/http/api/common"
	accesssvc "minio-web/api/src/services/access"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	runtimesvc "minio-web/api/src/services/runtime"
)

func HandleAccessVersion(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "GET" {
		return apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
	}
	httpcommon.WriteJSON(w, r, 200, accesssvc.AccessRevision{Revision: app.AccessRevision(s)})
	return nil
}

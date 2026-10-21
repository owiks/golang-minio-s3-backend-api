package controllers

import (
	"net/http"

	httpcommon "minio-web/api/src/http/api/common"
	httpvalidation "minio-web/api/src/http/validation"
	"minio-web/api/src/services/apperror"
	archivessvc "minio-web/api/src/services/archives"
	"minio-web/api/src/services/messages"
	runtimesvc "minio-web/api/src/services/runtime"
)

func HandlePrepareArchive(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "POST" {
		return apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
	}

	var body archivessvc.PrepareArchiveRequest
	if err := httpvalidation.DecodeLimit(w, r, &body, 2*1024*1024); err != nil {
		return err
	}
	value, err := archivessvc.PrepareArchive(r.Context(), app.Archives, runtimesvc.NewClients(s), body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

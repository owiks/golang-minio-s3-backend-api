package controllers

import (
	"net/http"

	httpcommon "minio-web/api/src/http/api/common"
	httpvalidation "minio-web/api/src/http/validation"
	"minio-web/api/src/services/apperror"
	folderssvc "minio-web/api/src/services/folders"
	"minio-web/api/src/services/messages"
	runtimesvc "minio-web/api/src/services/runtime"
)

func HandleCreateFolder(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "POST" {
		return apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
	}

	var body folderssvc.CreateFolderRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	value, err := folderssvc.CreateFolder(r.Context(), app.MutationLocker(s), runtimesvc.NewClients(s), body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 201, value)
	return nil
}

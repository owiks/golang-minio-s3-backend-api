package controllers

import (
	"net/http"

	httpcommon "minio-web/api/src/http/api/common"
	httpvalidation "minio-web/api/src/http/validation"
	"minio-web/api/src/services/apperror"
	searchsvc "minio-web/api/src/services/files"
	"minio-web/api/src/services/messages"
	runtimesvc "minio-web/api/src/services/runtime"
	searchtypes "minio-web/api/src/services/search"
)

func HandleContentSearch(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "POST" {
		return apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
	}

	var body searchtypes.ContentSearchBody
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	value, err := searchsvc.SearchContent(r.Context(), runtimesvc.NewClients(s), body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

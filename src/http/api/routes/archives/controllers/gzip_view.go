package controllers

import (
	"net/http"

	httpcommon "minio-web/api/src/http/api/common"
	"minio-web/api/src/services/apperror"
	archivessvc "minio-web/api/src/services/archives"
	runtimesvc "minio-web/api/src/services/runtime"
)

func HandleGzipView(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "GET" {
		return apperror.New(405, "MethodNotAllowed", "Метод не поддерживается.")
	}
	value, err := archivessvc.ReadGzip(r.Context(), app, runtimesvc.NewClients(s), r.URL.Query().Get("bucket"), r.URL.Query().Get("key"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}

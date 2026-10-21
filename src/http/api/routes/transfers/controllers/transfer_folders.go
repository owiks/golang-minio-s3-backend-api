package controllers

import (
	"net/http"

	httpcommon "minio-web/api/src/http/api/common"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	runtimesvc "minio-web/api/src/services/runtime"
	transferssvc "minio-web/api/src/services/transfers"
)

func HandleTransferList(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "GET" {
		return apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
	}
	q := r.URL.Query()
	value, err := transferssvc.PrepareFolderTransfer(r.Context(), runtimesvc.NewClients(s), transferssvc.PrepareFolderOptions{
		Bucket: q.Get("bucket"), Prefix: q.Get("prefix"), VersionRequested: q.Has("version") || q.Has("versionId"),
	})
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleTransferPrepare(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "GET" {
		return apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
	}
	q := r.URL.Query()
	value, err := transferssvc.PrepareDestination(r.Context(), runtimesvc.NewClients(s), q.Get("bucket"), q.Get("key"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

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

func validateFolderDelete(r *http.Request) error {
	q := r.URL.Query()
	if q.Has("version") || q.Has("versionId") {
		return apperror.New(400, "InvalidFolderDelete", messages.HttpInvalidFolderDelete)
	}
	return nil
}
func HandleInspectFolderDeletion(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if err := validateFolderDelete(r); err != nil {
		return err
	}
	q := r.URL.Query()
	var value any
	var err error
	if q.Has("key") {
		value, err = folderssvc.InspectFolderObject(r.Context(), runtimesvc.NewClients(s), q.Get("bucket"), q.Get("prefix"), q.Get("key"))
	} else {
		value, err = folderssvc.InspectFolder(r.Context(), runtimesvc.NewClients(s), q.Get("bucket"), q.Get("prefix"))
	}
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleDeleteFolderObject(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var body folderssvc.FolderDeleteRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	if err := validateFolderDelete(r); err != nil {
		return err
	}
	q := r.URL.Query()
	value, err := folderssvc.DeleteFolderObject(r.Context(), runtimesvc.NewClients(s), q.Get("bucket"), q.Get("prefix"), q.Get("key"), body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleListFolderShares(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	value, err := folderssvc.ListFolderShares(r.Context(), app.Shares, s.SessionID(), r.URL.Query().Get("bucket"), r.URL.Query().Get("prefix"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleCreateFolderShare(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var body folderssvc.FolderSharesRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	value, err := folderssvc.CreateFolderShare(r.Context(), app.Shares, runtimesvc.NewClients(s), body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 201, value)
	return nil
}
func HandleRevokeFolderShare(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	value, err := folderssvc.RevokeFolderShare(r.Context(), app.Shares, s.SessionID(), r.URL.Query().Get("token"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

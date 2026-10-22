package controllers

import (
	"net/http"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	httpvalidation "github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	folderssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/folders"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
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
	ttl, err := s.DelegationTTL(r.Context(), "objects:read", body.Bucket, body.Prefix, body.ExpiresIn)
	if err != nil {
		return err
	}
	body.ExpiresIn = ttl
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

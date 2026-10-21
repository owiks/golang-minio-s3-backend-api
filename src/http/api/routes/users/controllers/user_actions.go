package controllers

import (
	"net/http"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	httpvalidation "github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/identities"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func HandleListUsers(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	value, err := identities.ListUsers(r.Context(), runtimesvc.NewClients(s), s.ActorName())
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

func HandleGetUser(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, name string) error {
	value, err := identities.GetUser(r.Context(), runtimesvc.NewClients(s), s.ActorName(), name)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

func HandleCreateUser(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var b identities.CreateUserRequest
	if err := httpvalidation.Decode(w, r, &b); err != nil {
		return err
	}
	if err := identities.CreateUser(r.Context(), runtimesvc.NewClients(s), b); err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, httpcommon.Success{OK: true})
	return nil
}

func HandleSetUserStatus(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, name string) error {
	var b identities.StatusRequest
	if err := httpvalidation.Decode(w, r, &b); err != nil {
		return err
	}
	if err := identities.SetUserStatus(r.Context(), runtimesvc.NewClients(s), s.ActorName(), name, b); err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, httpcommon.Success{OK: true})
	return nil
}

func HandleDeleteUser(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, name string) error {
	if err := httpvalidation.Confirmation(w, r, name); err != nil {
		return err
	}
	if err := identities.DeleteUser(r.Context(), runtimesvc.NewClients(s), s.ActorName(), name); err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, httpcommon.Success{OK: true})
	return nil
}

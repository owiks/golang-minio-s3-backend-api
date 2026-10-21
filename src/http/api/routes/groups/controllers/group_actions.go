package controllers

import (
	"net/http"

	httpcommon "minio-web/api/src/http/api/common"
	httpvalidation "minio-web/api/src/http/validation"
	"minio-web/api/src/services/identities"
	runtimesvc "minio-web/api/src/services/runtime"
)

func HandleListGroups(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	value, err := identities.ListGroups(r.Context(), runtimesvc.NewClients(s))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleGetGroup(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, name string) error {
	value, err := identities.GetGroup(r.Context(), runtimesvc.NewClients(s), name)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleCreateGroup(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var b identities.CreateGroupRequest
	if err := httpvalidation.Decode(w, r, &b); err != nil {
		return err
	}
	if err := identities.CreateGroup(r.Context(), runtimesvc.NewClients(s), b); err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, httpcommon.Success{OK: true})
	return nil
}
func HandleUpdateGroupMembers(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, name string) error {
	var b identities.GroupMembersRequest
	if err := httpvalidation.Decode(w, r, &b); err != nil {
		return err
	}
	if err := identities.UpdateGroupMembers(r.Context(), runtimesvc.NewClients(s), name, b); err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, httpcommon.Success{OK: true})
	return nil
}
func HandleSetGroupStatus(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, name string) error {
	var b identities.StatusRequest
	if err := httpvalidation.Decode(w, r, &b); err != nil {
		return err
	}
	if err := identities.SetGroupStatus(r.Context(), runtimesvc.NewClients(s), name, b); err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, httpcommon.Success{OK: true})
	return nil
}
func HandleDeleteGroup(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, name string) error {
	if err := httpvalidation.Confirmation(w, r, name); err != nil {
		return err
	}
	if err := identities.DeleteGroup(r.Context(), runtimesvc.NewClients(s), name); err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, httpcommon.Success{OK: true})
	return nil
}

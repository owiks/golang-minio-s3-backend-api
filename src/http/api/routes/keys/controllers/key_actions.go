package controllers

import (
	"net/http"

	httpcommon "minio-web/api/src/http/api/common"
	httpvalidation "minio-web/api/src/http/validation"
	keyssvc "minio-web/api/src/services/keys"
	runtimesvc "minio-web/api/src/services/runtime"
)

func HandleListKeys(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, temporary bool) error {
	value, err := keyssvc.ListKeys(r.Context(), runtimesvc.NewClients(s), temporary, r.URL.Query().Get("scope"), r.URL.Query().Get("parent"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleGetKey(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, name string, temporary bool) error {
	value, err := keyssvc.KeyDetail(r.Context(), runtimesvc.NewClients(s), name, temporary)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleCreateKey(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var b keyssvc.KeyInput
	if err := httpvalidation.Decode(w, r, &b); err != nil {
		return err
	}
	value, err := keyssvc.CreateKey(r.Context(), runtimesvc.NewClients(s), b)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleEditKey(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, name string) error {
	if err := keyssvc.EnsureMutable(runtimesvc.NewClients(s), name); err != nil {
		return err
	}
	var b keyssvc.KeyInput
	if err := httpvalidation.Decode(w, r, &b); err != nil {
		return err
	}
	if err := keyssvc.EditKey(r.Context(), runtimesvc.NewClients(s), name, b); err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, httpcommon.Success{OK: true})
	return nil
}
func HandleSetKeyStatus(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, name string) error {
	if err := keyssvc.EnsureMutable(runtimesvc.NewClients(s), name); err != nil {
		return err
	}
	var b keyssvc.StatusRequest
	if err := httpvalidation.Decode(w, r, &b); err != nil {
		return err
	}
	if err := keyssvc.SetKeyStatus(r.Context(), runtimesvc.NewClients(s), name, b); err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, httpcommon.Success{OK: true})
	return nil
}
func HandleRotateKeySecret(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, name string) error {
	if err := keyssvc.EnsureMutable(runtimesvc.NewClients(s), name); err != nil {
		return err
	}
	var b keyssvc.RotateRequest
	if err := httpvalidation.Decode(w, r, &b); err != nil {
		return err
	}
	value, err := keyssvc.RotateKeySecret(r.Context(), runtimesvc.NewClients(s), name, b)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleDeleteKey(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, name string) error {
	if err := keyssvc.EnsureMutable(runtimesvc.NewClients(s), name); err != nil {
		return err
	}
	if err := httpvalidation.Confirmation(w, r, name); err != nil {
		return err
	}
	if err := keyssvc.DeleteKey(r.Context(), runtimesvc.NewClients(s), name); err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, httpcommon.Success{OK: true})
	return nil
}

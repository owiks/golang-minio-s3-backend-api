package controllers

import (
	httpcommon "minio-web/api/src/http/api/common"
	httpvalidation "minio-web/api/src/http/validation"
	svc "minio-web/api/src/services/identities"
	runtimesvc "minio-web/api/src/services/runtime"
	"net/http"
)

func HandleGetBindings(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	q := r.URL.Query()
	value, err := svc.GetBindings(r.Context(), runtimesvc.NewClients(s), q.Get("user"), q.Get("group"), q.Get("policy"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}
func HandleSaveBinding(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var body svc.BindingRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	if err := svc.SaveBinding(r.Context(), runtimesvc.NewClients(s), body); err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, httpcommon.Success{OK: true})
	return nil
}

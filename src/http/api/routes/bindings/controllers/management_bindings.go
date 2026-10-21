package controllers

import (
	"net/http"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	httpvalidation "github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	svc "github.com/owiks/golang-minio-s3-backend-api/src/services/identities"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func HandleGetBindings(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	q := r.URL.Query()
	value, err := svc.GetBindings(r.Context(), runtimesvc.NewClients(s), q.Get("user"), q.Get("group"), q.Get("policy"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
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
	httpcommon.WriteJSON(w, r, 200, httpcommon.Success{OK: true})
	return nil
}

package controllers

import (
	"net/http"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	httpvalidation "github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/identities"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func HandleListPolicies(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	value, err := identities.ListPolicies(r.Context(), runtimesvc.NewClients(s))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleGetPolicy(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, name string) error {
	value, err := identities.GetPolicy(r.Context(), runtimesvc.NewClients(s), name)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleCreatePolicy(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var b identities.PolicyRequest
	if err := httpvalidation.Decode(w, r, &b); err != nil {
		return err
	}
	if err := identities.CreatePolicy(r.Context(), runtimesvc.NewClients(s), identities.CreatePolicyCommand{Name: b.Name, Document: b.Document, AcknowledgeNonAtomicCreate: b.AcknowledgeNonAtomicCreate}); err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, httpcommon.Success{OK: true})
	return nil
}
func HandleUpdatePolicy(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, name string) error {
	var b identities.PolicyRequest
	if err := httpvalidation.Decode(w, r, &b); err != nil {
		return err
	}
	if err := identities.UpdatePolicy(r.Context(), runtimesvc.NewClients(s), name, identities.UpdatePolicyCommand{Document: b.Document, Revision: b.Revision}); err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, httpcommon.Success{OK: true})
	return nil
}
func HandleDeletePolicy(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, name string) error {
	if err := httpvalidation.Confirmation(w, r, name); err != nil {
		return err
	}
	if err := identities.DeletePolicy(r.Context(), runtimesvc.NewClients(s), name); err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, httpcommon.Success{OK: true})
	return nil
}

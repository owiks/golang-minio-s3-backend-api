package controllers

import (
	httpcommon "minio-web/api/src/http/api/common"
	httpvalidation "minio-web/api/src/http/validation"
	"minio-web/api/src/services/apperror"
	runtimesvc "minio-web/api/src/services/runtime"
	tierssvc "minio-web/api/src/services/tiers"
	"net/http"
)

func HandleListTiers(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	value, err := tierssvc.ListTiers(r.Context(), runtimesvc.NewClients(s))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}
func HandleCreateOrVerifyTier(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var body tierssvc.TiersRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	if body.Operation == "verify" {
		value, err := tierssvc.VerifyTier(r.Context(), app.Config().Endpoints.Allowed, runtimesvc.NewClients(s), tierssvc.VerifyTierCommand{Name: body.Name})
		if err != nil {
			return err
		}
		httpcommon.WriteJSON(w, 200, value)
		return nil
	}
	if body.Operation != "" {
		return apperror.New(400, "InvalidTierOperation", "Неизвестная операция.")
	}
	value, err := tierssvc.CreateTier(r.Context(), app.Config().Endpoints.Allowed, runtimesvc.NewClients(s), tierssvc.CreateTierCommand{Name: body.Name, Type: body.Type, Endpoint: body.Endpoint, Bucket: body.Bucket, Prefix: body.Prefix, Region: body.Region, StorageClass: body.StorageClass, AccessKey: body.AccessKey, SecretKey: body.SecretKey})
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}
func HandleUpdateTier(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var body tierssvc.TiersRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	value, err := tierssvc.UpdateTierCredentials(r.Context(), app.Config().Endpoints.Allowed, runtimesvc.NewClients(s), tierssvc.UpdateTierCredentialsCommand{Name: body.Name, Revision: body.Revision, Confirmation: body.Confirmation, AccessKey: body.AccessKey, SecretKey: body.SecretKey})
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}
func HandleRemoveTier(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var body tierssvc.TiersRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	value, err := tierssvc.RemoveTier(r.Context(), app.Config().Endpoints.Allowed, runtimesvc.NewClients(s), tierssvc.RemoveTierCommand{Name: body.Name, Revision: body.Revision, Confirmation: body.Confirmation})
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}

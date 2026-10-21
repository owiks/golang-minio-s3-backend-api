package controllers

import (
	"net/http"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	httpvalidation "github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	serversvc "github.com/owiks/golang-minio-s3-backend-api/src/services/server"
)

func HandleReadServerConfig(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	value, err := serversvc.LoadConfig(r.Context(), runtimesvc.NewClients(s), r.URL.Query().Get("subsystem"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleSaveServerConfig(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var body serversvc.ConfigChangeRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	value, err := serversvc.SaveConfig(r.Context(), runtimesvc.NewClients(s), r.URL.Query().Get("subsystem"), body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleReadServerPools(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	value, _, err := serversvc.ReadPools(r.Context(), runtimesvc.NewClients(s))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleChangeServerPools(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var body serversvc.PoolChangeRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	command, err := body.Command()
	if err != nil {
		return err
	}
	if err := serversvc.ChangePools(r.Context(), runtimesvc.NewClients(s), command); err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, httpcommon.Success{OK: true})
	return nil
}

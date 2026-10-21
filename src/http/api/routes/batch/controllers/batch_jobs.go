package controllers

import (
	httpcommon "minio-web/api/src/http/api/common"
	httpvalidation "minio-web/api/src/http/validation"
	svc "minio-web/api/src/services/batch"
	runtimesvc "minio-web/api/src/services/runtime"
	"net/http"
)

func HandleListBatchJobs(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	value, err := svc.List(r.Context(), runtimesvc.NewClients(s))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}

func HandleStartBatchJob(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var body svc.BatchInput
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	value, err := svc.Start(r.Context(), runtimesvc.NewClients(s), body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}

func HandleBatchJobStatus(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, id string) error {
	value, err := svc.Status(r.Context(), runtimesvc.NewClients(s), id)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}

func HandleCancelBatchJob(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, id string) error {
	if err := httpvalidation.Confirmation(w, r, id); err != nil {
		return err
	}
	if err := svc.Cancel(r.Context(), runtimesvc.NewClients(s), id); err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, httpcommon.Success{OK: true})
	return nil
}

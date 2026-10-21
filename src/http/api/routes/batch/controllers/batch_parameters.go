package controllers

import (
	"net/http"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	batchsvc "github.com/owiks/golang-minio-s3-backend-api/src/services/batch"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func HandleBatchParameters(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, id string) error {
	value, err := batchsvc.ReadBatchParameters(r.Context(), runtimesvc.NewClients(s), id)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

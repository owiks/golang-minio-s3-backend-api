package controllers

import (
	"net/http"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func Status(app *runtime.State, w http.ResponseWriter, r *http.Request, _ *runtime.Session, _ string) error {
	value, err := app.OperatorStatus(r.Context())
	if err != nil {
		return err
	}
	common.WriteJSON(w, r, http.StatusOK, value)
	return nil
}

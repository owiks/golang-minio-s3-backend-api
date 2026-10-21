package controllers

import (
	"net/http"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	serversvc "github.com/owiks/golang-minio-s3-backend-api/src/services/server"
)

func HandleServerInfo(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	value, err := serversvc.ReadServerInfo(r.Context(), runtimesvc.NewClients(s))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

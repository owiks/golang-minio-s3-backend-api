package controllers

import (
	"net/http"

	httpcommon "minio-web/api/src/http/api/common"
	runtimesvc "minio-web/api/src/services/runtime"
	serversvc "minio-web/api/src/services/server"
)

func HandleServerInfo(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	value, err := serversvc.ReadServerInfo(r.Context(), runtimesvc.NewClients(s))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

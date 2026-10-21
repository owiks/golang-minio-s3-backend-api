package controllers

import (
	"net/http"

	httpcommon "minio-web/api/src/http/api/common"
	accesssvc "minio-web/api/src/services/access"
	"minio-web/api/src/services/apperror"
	runtimesvc "minio-web/api/src/services/runtime"
)

func HandleAccess(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "GET" {
		return apperror.New(405, "MethodNotAllowed", "Метод не поддерживается.")
	}
	revision := app.AccessRevision(s)
	buckets, access, err := accesssvc.DiscoverAccess(r.Context(), runtimesvc.NewClients(s))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, accesssvc.AccessSnapshot{Buckets: buckets, Access: access, Revision: revision})
	return nil
}

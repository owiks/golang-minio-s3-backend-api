package controllers

import (
	"net/http"

	httpcommon "minio-web/api/src/http/api/common"
	"minio-web/api/src/services/identities"
	runtimesvc "minio-web/api/src/services/runtime"
)

func HandlePolicySummaries(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	rows, err := identities.PolicySummaries(r.Context(), runtimesvc.NewClients(s))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, rows)
	return nil
}

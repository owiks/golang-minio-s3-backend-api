package controllers

import (
	"net/http"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/identities"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func HandlePolicySummaries(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	rows, err := identities.PolicySummaries(r.Context(), runtimesvc.NewClients(s))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, rows)
	return nil
}

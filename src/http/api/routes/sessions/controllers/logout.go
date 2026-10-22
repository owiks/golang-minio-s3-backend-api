package controllers

import (
	"net/http"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
)

func HandleLogout(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session) error {
	if err := app.CloseAccess(r.Context(), s); err != nil {
		return err
	}
	telemetry.Event(r.Context(), "session closed")
	if !s.UsesBearer() {
		http.SetCookie(w, common.SessionCookie(s, r, true))
	}
	common.WriteJSON(w, r, 200, common.Success{OK: true})
	return nil
}

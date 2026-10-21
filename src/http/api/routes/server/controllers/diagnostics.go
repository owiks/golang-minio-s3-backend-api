package controllers

import (
	"net/http"
	"strconv"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	serversvc "github.com/owiks/golang-minio-s3-backend-api/src/services/server"
)

func HandleDiagnostics(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "GET" {
		return apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
	}
	q := r.URL.Query()
	seconds, e1 := strconv.Atoi(q.Get("seconds"))
	limit, e2 := strconv.Atoi(q.Get("limit"))
	if e1 != nil || e2 != nil {
		return serversvc.InvalidDiagnostic()
	}
	report, err := serversvc.ReadDiagnostics(r.Context(), runtimesvc.NewClients(s), serversvc.DiagnosticOptions{Kind: q.Get("kind"), Level: q.Get("level"), Seconds: seconds, Limit: limit})
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, report)
	return nil
}

package controllers

import (
	"net/http"
	"strconv"

	httpcommon "minio-web/api/src/http/api/common"
	"minio-web/api/src/services/apperror"
	runtimesvc "minio-web/api/src/services/runtime"
	serversvc "minio-web/api/src/services/server"
)

func HandleDiagnostics(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "GET" {
		return apperror.New(405, "MethodNotAllowed", "Метод не поддерживается.")
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
	httpcommon.WriteJSON(w, 200, report)
	return nil
}

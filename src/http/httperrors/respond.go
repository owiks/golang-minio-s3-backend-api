package httperrors

import (
	"net/http"

	"minio-web/api/src/http/api/common"
	"minio-web/api/src/services/apperror"
)

func Respond(w http.ResponseWriter, r *http.Request, err error) {
	problem := apperror.Public(err)
	// The wrapper exposes a request context without changing streaming writers.
	if observed, ok := w.(interface{ Failure(string) }); ok {
		observed.Failure(problem.Code)
	}
	common.WriteJSON(w, r, problem.Status, map[string]any{"error": problem})
}

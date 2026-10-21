package status

import (
	"net/http"

	"minio-web/api/src/http/api/common"
	"minio-web/api/src/http/healthresponse"
)

func Health(w http.ResponseWriter, r *http.Request) {
	common.WriteJSON(w, http.StatusOK, healthresponse.Response{OK: true})
}

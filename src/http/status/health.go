package status

import (
	"net/http"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/healthresponse"
)

func Health(w http.ResponseWriter, r *http.Request) {
	common.WriteJSON(w, r, http.StatusOK, healthresponse.Response{OK: true})
}

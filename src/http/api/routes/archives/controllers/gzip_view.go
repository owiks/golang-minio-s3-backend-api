package controllers

import (
	"net/http"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	archivessvc "github.com/owiks/golang-minio-s3-backend-api/src/services/archives"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func HandleGzipView(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "GET" {
		return apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
	}
	value, err := archivessvc.ReadGzip(r.Context(), app, runtimesvc.NewClients(s), r.URL.Query().Get("bucket"), r.URL.Query().Get("key"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

package controllers

import (
	"context"
	"net/http"
	"time"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	serversvc "github.com/owiks/golang-minio-s3-backend-api/src/services/server"
)

func HandleServerMetrics(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "GET" {
		return apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	data, e := serversvc.ReadLiveMetrics(ctx, runtimesvc.NewClients(s))
	if e != nil {
		return e
	}
	httpcommon.WriteJSON(w, r, 200, data)
	return nil
}

package controllers

import (
	"errors"
	"net/http"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	httpvalidation "github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/commandlog"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	transferssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
)

func HandleFileTransfer(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "POST" {
		return apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
	}
	var body transferssvc.FileTransferRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	if len(r.Header.Values("Idempotency-Key")) > 1 || r.Header.Get("Idempotency-Key") == "" && len(r.Header.Values("Idempotency-Key")) != 0 {
		return apperror.New(400, "InvalidIdempotencyKey", messages.CommandInvalid)
	}
	value, id, replayed, err := transferssvc.TransferCommand(r.Context(), app.Commands, runtimesvc.NewClients(s), commandlog.Scope{ActorID: s.SessionID(), ConnectionID: s.ConnectionID(), Operation: "objects/transfer"}, r.Header.Get("Idempotency-Key"), body)
	if id != "" {
		w.Header().Set("X-Command-ID", id)
	}
	if replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	if err != nil {
		var issue *apperror.Error
		if errors.As(err, &issue) && (issue.Code == "CommandInProgress" || issue.Status == 429) {
			w.Header().Set("Retry-After", "1")
		}
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

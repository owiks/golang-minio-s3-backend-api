package controllers

import (
	"net/http"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	httpvalidation "github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	searchsvc "github.com/owiks/golang-minio-s3-backend-api/src/services/files"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	searchtypes "github.com/owiks/golang-minio-s3-backend-api/src/services/search"
)

func HandleContentSearch(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "POST" {
		return apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
	}

	var body searchtypes.ContentSearchBody
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	value, err := searchsvc.SearchContent(r.Context(), runtimesvc.NewClients(s), body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

package controllers

import (
	"net/http"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/capabilities"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func HandleCapabilities(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session) error {
	q := r.URL.Query()
	value, err := capabilities.Report(r.Context(), runtime.NewClients(s), app.CapabilityCache(s), capabilities.Request{
		Scope: capabilities.Scope{Bucket: q.Get("bucket"), Key: q.Get("key"), VersionID: q.Get("versionId")}, Feature: capabilities.Identifier(q.Get("feature")),
	})
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, http.StatusOK, value)
	return nil
}

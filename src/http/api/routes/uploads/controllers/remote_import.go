package controllers

import (
	"context"
	"net/http"

	"minio-web/api/src/http/stream"
	httpvalidation "minio-web/api/src/http/validation"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	objectssvc "minio-web/api/src/services/objects"
	objectwrite "minio-web/api/src/services/objects/write"
	"minio-web/api/src/services/remotesource"
	"minio-web/api/src/services/result"
	runtimesvc "minio-web/api/src/services/runtime"
	"minio-web/api/src/services/telemetry"
	uploadssvc "minio-web/api/src/services/uploads"
)

func HandleRemoteImport(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	client := remotesource.New()
	defer client.Close()
	return HandleImportFromURL(app, w, r, s, client)
}
func HandleImportFromURL(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, client uploadssvc.RemoteSource) error {
	if r.Method != "POST" {
		return apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
	}
	bucket, key, err := objectssvc.ObjectParams(r.URL.Query().Get("bucket"), r.URL.Query().Get("key"))
	if err != nil {
		return err
	}
	var body uploadssvc.ImportRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	job, err := uploadssvc.PrepareImport(r.Context(), runtimesvc.NewClients(s), bucket, key, body)
	if err != nil {
		return err
	}
	stream.WriteNDJSON(r.Context(), w, func(ctx context.Context, send func(result.StreamEvent[uploadssvc.ImportResult]) bool) error {
		size, err := uploadssvc.Import(ctx, app.Temporary, client, job, func(v result.StreamEvent[uploadssvc.ImportResult]) { send(v) })
		if err != nil {
			return objectwrite.FileWriteError(err)
		}
		telemetry.Outcome(ctx, "success", "")
		send(result.StreamEvent[uploadssvc.ImportResult]{Result: &uploadssvc.ImportResult{OK: true, Size: size}})
		return nil
	})
	return nil
}

package controllers

import (
	"mime"
	"net/http"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/presentation"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/stream"
	httpvalidation "github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	archivessvc "github.com/owiks/golang-minio-s3-backend-api/src/services/archives"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func HandleArchiveStatus(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, ticket string) error {
	value, err := archivessvc.ArchiveStatus(app.Archives, s.SessionID(), ticket)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleForgetArchive(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, ticket string) error {
	value, err := archivessvc.ForgetArchive(app.Archives, s.SessionID(), ticket)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleCancelArchive(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, ticket string) error {
	value, err := archivessvc.CancelArchive(app.Archives, s.SessionID(), ticket)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleConfigureArchive(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, ticket string) error {
	var body archivessvc.ArchiveRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	value, err := archivessvc.ConfigureArchive(app.Archives, s.SessionID(), ticket, body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleDownloadArchive(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, ticket string) error {
	download, err := archivessvc.StartDownload(r.Context(), app.Archives, runtimesvc.NewClients(s), ticket)
	if err != nil {
		return err
	}
	defer download.Close()
	defer stream.InterruptOnCancel(download.Context(), w)()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": download.Name()}))
	// This is a generated archive with API messages in its manifest, not a
	// stored object's download whose Content-Language belongs to the object.
	presentation.LanguageHeaders(r.Context(), w.Header())
	if err := download.StreamTo(w, func(manifest archivessvc.Manifest) any { return presentation.Render(r.Context(), manifest) }); err != nil {
		panic(http.ErrAbortHandler)
	}
	return nil
}
func HandleInspectZIP(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	value, err := archivessvc.InspectZIP(r.Context(), app.ZIPs, runtimesvc.NewClients(s), r.URL.Query().Get("bucket"), r.URL.Query().Get("key"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleExtractZIPEntry(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var body archivessvc.ZipExplorerRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	value, err := archivessvc.ExtractZIPEntry(r.Context(), app.ZIPs, app.Temporary, runtimesvc.NewClients(s), r.URL.Query().Get("bucket"), r.URL.Query().Get("key"), body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

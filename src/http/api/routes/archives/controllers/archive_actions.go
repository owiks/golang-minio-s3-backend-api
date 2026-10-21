package controllers

import (
	"mime"
	httpcommon "minio-web/api/src/http/api/common"
	"minio-web/api/src/http/stream"
	httpvalidation "minio-web/api/src/http/validation"
	archivessvc "minio-web/api/src/services/archives"
	runtimesvc "minio-web/api/src/services/runtime"
	"net/http"
)

func HandleArchiveStatus(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, ticket string) error {
	value, err := archivessvc.ArchiveStatus(app.Archives, s.SessionID(), ticket)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}
func HandleForgetArchive(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, ticket string) error {
	value, err := archivessvc.ForgetArchive(app.Archives, s.SessionID(), ticket)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}
func HandleCancelArchive(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, ticket string) error {
	value, err := archivessvc.CancelArchive(app.Archives, s.SessionID(), ticket)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
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
	httpcommon.WriteJSON(w, 200, value)
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
	if err := download.StreamTo(w); err != nil {
		panic(http.ErrAbortHandler)
	}
	return nil
}
func HandleInspectZIP(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	value, err := archivessvc.InspectZIP(r.Context(), app.ZIPs, runtimesvc.NewClients(s), r.URL.Query().Get("bucket"), r.URL.Query().Get("key"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
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
	httpcommon.WriteJSON(w, 200, value)
	return nil
}

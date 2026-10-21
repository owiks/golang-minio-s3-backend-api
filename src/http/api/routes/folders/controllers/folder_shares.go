package controllers

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/stream"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	folderssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/folders"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
)

func HandlePublicFolder(app *runtimesvc.State, w http.ResponseWriter, r *http.Request) error {
	if r.Method != "GET" {
		return apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
	}
	q := r.URL.Query()
	value, err := folderssvc.ReadPublicFolder(r.Context(), app.Shares, app, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), folderssvc.PublicFolderOptions{Mode: q.Get("mode"), Prefix: q.Get("prefix"), Cursor: q.Get("cursor"), Key: q.Get("key"), Kind: q.Get("kind")})
	if err != nil {
		return err
	}
	if preview := value.Preview; preview != nil {
		w.Header().Set("Content-Type", preview.ContentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(preview.Data)))
		_, err = w.Write(preview.Data)
		return err
	}
	switch {
	case value.Info != nil:
		httpcommon.WriteJSON(w, r, 200, value.Info)
	case value.Page != nil:
		httpcommon.WriteJSON(w, r, 200, value.Page)
	case value.Link != nil:
		httpcommon.WriteJSON(w, r, 200, value.Link)
	default:
		return apperror.New(500, "InvalidFolderResult", messages.HttpInvalidFolderResult)
	}
	return nil
}
func HandleDownloadPublicFolderFile(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, id string) error {
	return servePublicFolderFile(app, w, r, id, false)
}
func HandleHeadPublicFolderFile(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, id string) error {
	return servePublicFolderFile(app, w, r, id, true)
}
func servePublicFolderFile(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, id string, head bool) error {
	file, err := folderssvc.OpenSharedFile(r.Context(), app.Shares, app, id, !head, r.Header.Get("Range"))
	if err != nil {
		var rangeErr *folderssvc.RangeError
		if errors.As(err, &rangeErr) {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", rangeErr.Size))
		}
		return err
	}
	defer file.Close()
	info := file.Summary()
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(30 * time.Minute))
	defer stream.InterruptOnCancel(file.Context(), w)()
	if info.Status == 206 {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", info.Start, info.End, info.Size))
	}
	if info.Media {
		w.Header().Set("Accept-Ranges", "bytes")
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(info.Disposition, map[string]string{"filename": info.Name}))
	w.Header().Set("Content-Type", info.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(max(0, info.End-info.Start+1), 10))
	w.WriteHeader(info.Status)
	if head {
		return nil
	}
	n, err := file.StreamTo(w)
	if err != nil || n != max(0, info.End-info.Start+1) {
		if file.Context().Err() != nil {
			telemetry.Outcome(r.Context(), "cancelled", "SharedReadCancelled")
		}
		panic(http.ErrAbortHandler)
	}
	return nil
}

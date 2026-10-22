package controllers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	httpvalidation "github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	objectssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
	objectwrite "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/write"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	uploadssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/uploads"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads/integrity"
)

func HandlePrepareFile(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	bucket, key, err := objectssvc.ObjectParams(r.URL.Query().Get("bucket"), r.URL.Query().Get("key"))
	if err != nil {
		return err
	}
	value, err := uploadssvc.PrepareFile(r.Context(), runtimesvc.NewClients(s), bucket, key)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleWriteFile(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	bucket, key, err := objectssvc.ObjectParams(r.URL.Query().Get("bucket"), r.URL.Query().Get("key"))
	if err != nil {
		return err
	}
	size, err := strconv.ParseInt(r.Header.Get("X-Upload-Size"), 10, 64)
	if err != nil || size < 0 || size > objectwrite.MaxFileSize || r.ContentLength != size {
		return apperror.New(400, "InvalidUploadSize", messages.HttpInvalidUploadSize)
	}
	r.Body = http.MaxBytesReader(w, r.Body, objectwrite.MaxFileSize)
	var expected *integrity.Spec
	algorithm, checksum := r.Header.Get("X-Upload-Checksum-Algorithm"), r.Header.Get("X-Upload-Checksum-Value")
	encoding, kind := r.Header.Get("X-Upload-Checksum-Encoding"), r.Header.Get("X-Upload-Checksum-Type")
	if algorithm != "" || checksum != "" || encoding != "" || kind != "" {
		expected = &integrity.Spec{Algorithm: algorithm, Value: checksum, Encoding: encoding, Type: kind, Scope: integrity.Object}
	}
	value, err := uploadssvc.WriteFileWithChecksum(r.Context(), app.Temporary, runtimesvc.NewClients(s), bucket, key, r.Header.Get("X-Object-Revision"), r.Header.Get("Content-Type"), size, r.Body, expected)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleListIncompleteUploads(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	q := r.URL.Query()
	value, err := uploadssvc.ListIncompleteUploads(r.Context(), runtimesvc.NewClients(s), uploadssvc.IncompleteUploadOptions{Bucket: q.Get("bucket"), Prefix: q.Get("prefix"), KeyMarker: q.Get("keyMarker"), UploadIDMarker: q.Get("uploadIdMarker"), Summary: q.Get("summary") == "1"})
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleInspectIncompleteUpload(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	q := r.URL.Query()
	value, err := uploadssvc.InspectIncompleteUpload(r.Context(), runtimesvc.NewClients(s), uploadssvc.IncompleteUploadOptions{Bucket: q.Get("bucket"), Key: q.Get("key"), UploadID: q.Get("uploadId"), Summary: q.Get("summary") == "1"})
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleAbortIncompleteUpload(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var body uploadssvc.IncompleteUploadsRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	value, err := uploadssvc.AbortIncompleteUpload(r.Context(), runtimesvc.NewClients(s), body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleListUploadInvites(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	value, err := uploadssvc.ListUploadInvites(r.Context(), app.Invites, s.SessionID(), r.URL.Query().Get("bucket"), r.URL.Query().Get("prefix"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleCreateUploadInvite(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var body uploadssvc.UploadInvitesRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	ttl, err := s.DelegationTTL(r.Context(), "objects:write", body.Bucket, body.Prefix, body.ExpiresIn)
	if err != nil {
		return err
	}
	body.ExpiresIn = ttl
	value, err := uploadssvc.CreateUploadInvite(r.Context(), app.Invites, runtimesvc.NewClients(s), body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 201, value)
	return nil
}
func HandleRevokeUploadInvite(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	value, err := uploadssvc.RevokeUploadInvite(r.Context(), app.Invites, s.SessionID(), r.URL.Query().Get("token"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func setReceiveDeadlines(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(30 * time.Minute))
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(30 * time.Minute))
}
func HandleInspectUploadInvite(app *runtimesvc.State, w http.ResponseWriter, r *http.Request) error {
	setReceiveDeadlines(w)
	value, err := uploadssvc.InspectUploadInvite(r.Context(), app.Invites, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleReceiveFile(app *runtimesvc.State, w http.ResponseWriter, r *http.Request) error {
	if !httpcommon.SameOrigin(r, app.Config().Origin) {
		return apperror.New(403, "OriginDenied", messages.HttpOriginDeniedUploadActions)
	}
	size, err := strconv.ParseInt(r.Header.Get("X-Upload-Size"), 10, 64)
	if err != nil {
		return apperror.New(400, "InvalidUpload", messages.HttpInvalidUpload)
	}
	setReceiveDeadlines(w)
	value, err := uploadssvc.ReceiveFile(r.Context(), app.Invites, app, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), r.URL.Query().Get("name"), r.Header.Get("Content-Type"), size, r.ContentLength, r.Body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

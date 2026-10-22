package controllers

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/commandlog"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
)

const bodyLimit = 1 << 20

type TransfersRequest struct {
	Items []transfers.FileTransferRequest `json:"items" validate:"required,min=1,max=1000"`
}
type ScanRequest struct {
	Bucket string `json:"bucket" validate:"required,maxBytes=63"`
	Prefix string `json:"prefix" validate:"maxBytes=1024"`
}
type RevisionRequest struct {
	Revision uint64 `json:"revision" validate:"required"`
}

func CreateTransfers(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, _ string) error {
	var req TransfersRequest
	if err := validation.DecodeLimit(w, r, &req, bodyLimit); err != nil {
		return err
	}
	return create(app, w, r, s, operations.Spec{Transfers: req.Items})
}
func CreateScan(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, _ string) error {
	var req ScanRequest
	if err := validation.DecodeLimit(w, r, &req, bodyLimit); err != nil {
		return err
	}
	return create(app, w, r, s, operations.Spec{Scan: &operations.ScanSpec{Bucket: req.Bucket, Prefix: req.Prefix}})
}
func create(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, spec operations.Spec) error {
	if !spec.Valid() {
		return operations.PublicError(operations.ErrInvalid)
	}
	if len(r.Header.Values("Idempotency-Key")) > 1 || r.Header.Get("Idempotency-Key") == "" && len(r.Header.Values("Idempotency-Key")) != 0 {
		return commandlog.PublicError(commandlog.ErrInvalid)
	}
	snapshot, id, replayed, err := app.SubmitOperation(r.Context(), s, spec, r.Header.Get("Idempotency-Key"))
	if id != "" {
		w.Header().Set("X-Command-ID", id)
	}
	if replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	if err != nil {
		if issue := apperror.Public(err); issue.Code == "CommandInProgress" || issue.Status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "1")
		}
		return err
	}
	w.Header().Set("Location", "/api/sessions/"+url.PathEscape(s.SessionID())+"/operations/"+url.PathEscape(snapshot.ID))
	return writeView(app, w, r, http.StatusAccepted, snapshot)
}
func scope(s *runtime.Session) operations.Scope {
	return operations.Scope{ActorID: s.SessionID(), ConnectionID: s.ConnectionID()}
}
func List(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, _ string) error {
	rows, err := app.Operations.List(r.Context(), scope(s))
	if err != nil {
		return operations.PublicError(err)
	}
	result := []View{}
	now := time.Now()
	until, err := app.Operations.Cooldown(r.Context(), s.ConnectionID(), now)
	if err != nil {
		return operations.PublicError(err)
	}
	for _, row := range rows {
		if err = app.AuthorizeOperation(r.Context(), s, row.Spec, false); err != nil {
			if apperror.Public(err).Status == http.StatusForbidden {
				continue
			}
			return err
		}
		result = append(result, pausedView(row, now, until))
	}
	common.WriteJSON(w, r, http.StatusOK, result)
	return nil
}
func operationID(r *http.Request) (string, error) {
	id := r.PathValue("operationID")
	if id == "" || len(id) > 64 || strings.ContainsAny(id, "/\x00\r\n") {
		return "", operations.PublicError(operations.ErrInvalid)
	}
	return id, nil
}
func Get(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, _ string) error {
	id, err := operationID(r)
	if err != nil {
		return err
	}
	snapshot, err := app.Operations.Get(r.Context(), scope(s), id)
	if err != nil {
		return operations.PublicError(err)
	}
	if err = app.AuthorizeOperation(r.Context(), s, snapshot.Spec, false); err != nil {
		return err
	}
	return writeView(app, w, r, http.StatusOK, snapshot)
}
func Cancel(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, _ string) error {
	id, revision, err := revisionRequest(w, r)
	if err != nil {
		return err
	}
	// The actor can stop its own work after resources were narrowed. No object
	// manifest or progress is returned from this ownership-only operation.
	if _, err = app.Operations.Cancel(r.Context(), scope(s), id, revision, time.Now()); err != nil {
		return operations.PublicError(err)
	}
	common.WriteJSON(w, r, http.StatusOK, common.Success{OK: true})
	return nil
}
func Retry(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, _ string) error {
	id, revision, err := revisionRequest(w, r)
	if err != nil {
		return err
	}
	snapshot, err := app.Operations.Get(r.Context(), scope(s), id)
	if err != nil {
		return operations.PublicError(err)
	}
	if err = app.AuthorizeOperation(r.Context(), s, snapshot.Spec, true); err != nil {
		return err
	}
	snapshot, err = app.Operations.Retry(r.Context(), scope(s), id, revision, time.Now())
	if err != nil {
		return operations.PublicError(err)
	}
	return writeView(app, w, r, http.StatusOK, snapshot)
}

func writeView(app *runtime.State, w http.ResponseWriter, r *http.Request, status int, snapshot operations.Snapshot) error {
	now := time.Now()
	var until time.Time
	if snapshot.State == operations.Queued {
		var err error
		until, err = app.Operations.Cooldown(r.Context(), snapshot.Scope.ConnectionID, now)
		if err != nil {
			return operations.PublicError(err)
		}
	}
	common.WriteJSON(w, r, status, pausedView(snapshot, now, until))
	return nil
}
func revisionRequest(w http.ResponseWriter, r *http.Request) (string, uint64, error) {
	id, err := operationID(r)
	if err != nil {
		return "", 0, err
	}
	var req RevisionRequest
	if err = validation.DecodeLimit(w, r, &req, bodyLimit); err != nil {
		return "", 0, err
	}
	if req.Revision == 0 {
		return "", 0, operations.PublicError(operations.ErrInvalid)
	}
	return id, req.Revision, nil
}

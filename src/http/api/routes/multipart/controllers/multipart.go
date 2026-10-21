package controllers

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"minio-web/api/src/http/api/common"
	httpvalidation "minio-web/api/src/http/validation"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/runtime"
	"minio-web/api/src/services/uploads/integrity"
	"minio-web/api/src/services/uploads/multipart"
)

func writeSnapshot(w http.ResponseWriter, s multipart.Snapshot, created bool) {
	status := 200
	if created {
		status = 201
	}
	if multipart.IsUncertain(s) {
		status = 202
	}
	common.WriteJSON(w, status, s)
}

func HandleCreate(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session) error {
	var input multipart.CreateRequest
	if err := httpvalidation.DecodeLimit(w, r, &input, 32*1024); err != nil {
		return err
	}
	value, err := multipart.Create(r.Context(), app.Multipart, app.MultipartClients(s), input)
	if err != nil {
		return err
	}
	writeSnapshot(w, value, true)
	return nil
}

func operation(rest string) (string, int, error) {
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || len(parts[0]) != 43 {
		return "", 0, apperror.New(400, "InvalidMultipartID", "Некорректный идентификатор загрузки.")
	}
	n := 0
	if len(parts) >= 3 && parts[1] == "parts" {
		var err error
		n, err = strconv.Atoi(parts[2])
		if err != nil || n < 1 || n > multipart.MaxParts {
			return "", 0, apperror.New(400, "InvalidPartNumber", "Некорректный номер части.")
		}
	}
	return parts[0], n, nil
}

func HandleInspect(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
	id, _, err := operation(rest)
	if err != nil {
		return err
	}
	value, err := multipart.Inspect(r.Context(), app.Multipart, app.MultipartClients(s), id)
	if err != nil {
		return err
	}
	writeSnapshot(w, value, false)
	return nil
}
func HandlePart(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
	id, n, err := operation(rest)
	if err != nil {
		return err
	}
	size, err := strconv.ParseInt(r.Header.Get("X-Upload-Size"), 10, 64)
	if err != nil || size < 0 || size > multipart.PartSize || r.ContentLength != size {
		return apperror.New(400, "InvalidPartSize", "Размер тела должен совпадать с X-Upload-Size и планом части.")
	}
	r.Body = &partBody{ReadCloser: http.MaxBytesReader(w, r.Body, multipart.PartSize), interrupt: func() { _ = http.NewResponseController(w).SetReadDeadline(time.Now()) }}
	spec := integrity.Spec{Algorithm: "SHA256", Encoding: "base64", Value: r.Header.Get("X-Part-Checksum-SHA256"), Type: "FULL_OBJECT", Scope: "PART"}
	value, err := multipart.PutPart(r.Context(), app.Multipart, app.MultipartClients(s), id, n, size, spec, r.Body)
	if err != nil {
		return err
	}
	writeSnapshot(w, value, false)
	return nil
}

// A server Body.Close can wait for a blocked Read. Setting this request's read
// deadline first makes operation cancellation (including logout) interrupt the
// socket read while preserving the independent response write deadline.
type partBody struct {
	io.ReadCloser
	once      sync.Once
	interrupt func()
	err       error
}

func (b *partBody) InterruptRead() { b.interrupt() }
func (b *partBody) Close() error {
	b.once.Do(func() { b.err = b.ReadCloser.Close() })
	return b.err
}
func HandleSign(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
	id, n, err := operation(rest)
	if err != nil {
		return err
	}
	var input multipart.PartRequest
	if err = httpvalidation.DecodeLimit(w, r, &input, 2048); err != nil {
		return err
	}
	value, err := multipart.SignPart(r.Context(), app.Multipart, app.MultipartClients(s), id, n, input)
	if err != nil {
		return err
	}
	common.WriteJSON(w, 200, value)
	return nil
}
func HandleComplete(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
	id, _, err := operation(rest)
	if err != nil {
		return err
	}
	value, err := multipart.Complete(r.Context(), app.Multipart, app.MultipartClients(s), id)
	if err != nil {
		return err
	}
	writeSnapshot(w, value, false)
	return nil
}
func HandleAbort(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
	id, _, err := operation(rest)
	if err != nil {
		return err
	}
	value, err := multipart.Abort(r.Context(), app.Multipart, app.MultipartClients(s), id)
	if err != nil {
		return err
	}
	writeSnapshot(w, value, false)
	return nil
}

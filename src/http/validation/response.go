package validation

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

func Decode(w http.ResponseWriter, r *http.Request, value any) error {
	return DecodeLimit(w, r, value, 64*1024)
}
func DecodeLimit(w http.ResponseWriter, r *http.Request, value any, limit int64) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return apperror.New(415, "JSONRequired", messages.HttpJSONRequired)
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return apperror.New(400, "InvalidJSON", messages.HttpInvalidJSON)
	}
	if d.Decode(new(any)) != io.EOF {
		return apperror.New(400, "InvalidJSON", messages.HttpInvalidJSONResponse)
	}
	return nil
}

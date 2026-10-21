package validation

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"minio-web/api/src/services/apperror"
)

func Decode(w http.ResponseWriter, r *http.Request, value any) error {
	return DecodeLimit(w, r, value, 64*1024)
}
func DecodeLimit(w http.ResponseWriter, r *http.Request, value any, limit int64) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return apperror.New(415, "JSONRequired", "Ожидается JSON-запрос.")
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return apperror.New(400, "InvalidJSON", "Некорректный JSON или превышен размер запроса.")
	}
	if d.Decode(new(any)) != io.EOF {
		return apperror.New(400, "InvalidJSON", "Ожидается один JSON-документ.")
	}
	return nil
}

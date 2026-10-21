package validation

import (
	"net/http"

	"minio-web/api/src/services/apperror"
)

func Confirmation(w http.ResponseWriter, r *http.Request, name string) error {
	var b ConfirmationRequest
	if err := Decode(w, r, &b); err != nil {
		return err
	}
	if b.Confirmation != name {
		return apperror.New(400, "ConfirmationRequired", "Для удаления введите точное имя ресурса.")
	}
	return nil
}

type ConfirmationRequest struct {
	Confirmation string `json:"confirmation" validate:"required"`
}

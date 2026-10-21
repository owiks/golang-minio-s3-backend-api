package validation

import (
	"net/http"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

func Confirmation(w http.ResponseWriter, r *http.Request, name string) error {
	var b ConfirmationRequest
	if err := Decode(w, r, &b); err != nil {
		return err
	}
	if b.Confirmation != name {
		return apperror.New(400, "ConfirmationRequired", messages.HttpConfirmationRequired)
	}
	return nil
}

type ConfirmationRequest struct {
	Confirmation string `json:"confirmation" validate:"required"`
}

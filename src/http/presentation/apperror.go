package presentation

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
)

type Error struct {
	apperror.Error
	Message string `json:"message"`
}

func presentApperrorError(localizer *localization.Localizer, v apperror.Error) Error {
	return Error{Error: v,
		Message: localizer.Render(v.Message),
	}
}

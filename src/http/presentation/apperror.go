package presentation

import (
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/localization"
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

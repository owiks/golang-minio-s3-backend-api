package presentation

import (
	"context"

	"minio-web/api/src/http/requestctx"
	"minio-web/api/src/services/apperror"
)

func Problem(ctx context.Context, problem *apperror.Error) *Error {
	if problem == nil {
		return nil
	}
	value := presentApperrorError(requestctx.Localizer(ctx), *problem)
	return &value
}

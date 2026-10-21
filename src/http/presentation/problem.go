package presentation

import (
	"context"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/requestctx"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
)

func Problem(ctx context.Context, problem *apperror.Error) *Error {
	if problem == nil {
		return nil
	}
	value := presentApperrorError(requestctx.Localizer(ctx), *problem)
	return &value
}

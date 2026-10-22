package operations

import (
	"errors"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

func PublicError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrInvalid):
		return apperror.New(400, "InvalidOperation", messages.OperationInvalid)
	case errors.Is(err, ErrNotFound):
		return apperror.New(404, "OperationNotFound", messages.OperationNotFound)
	case errors.Is(err, ErrConflict):
		return apperror.New(409, "OperationConflict", messages.OperationConflict)
	case errors.Is(err, ErrLimit):
		return apperror.New(429, "OperationLimit", messages.OperationLimit)
	default:
		return apperror.New(503, "OperationStoreUnavailable", messages.OperationUnavailable)
	}
}

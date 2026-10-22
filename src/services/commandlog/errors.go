package commandlog

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
		return apperror.New(400, "InvalidIdempotencyKey", messages.CommandInvalid)
	case errors.Is(err, ErrConflict):
		return apperror.New(409, "IdempotencyConflict", messages.CommandConflict)
	case errors.Is(err, ErrInProgress):
		return apperror.New(409, "CommandInProgress", messages.CommandRunning)
	case errors.Is(err, ErrUnknown):
		return apperror.New(409, "CommandOutcomeUncertain", messages.CommandUnknown)
	case errors.Is(err, ErrLimit):
		return apperror.New(429, "CommandLimit", messages.CommandLimit)
	default:
		return apperror.New(503, "CommandLogUnavailable", messages.CommandUnavailable)
	}
}

package authstate

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

func Unavailable() error { return apperror.New(503, "StateUnavailable", messages.AuthStateUnavailable) }
func Invalid() error     { return apperror.New(400, "InvalidAccessState", messages.AuthStateInvalid) }
func Denied() error      { return apperror.New(403, "AccessScopeDenied", messages.AuthStateDenied) }
func NotFound() error    { return apperror.New(404, "AccessStateNotFound", messages.AuthStateNotFound) }
func Conflict() error    { return apperror.New(409, "AccessStateConflict", messages.AuthStateConflict) }
func Limit() error       { return apperror.New(429, "AccessStateLimit", messages.AuthStateLimit) }

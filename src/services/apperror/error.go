// Package apperror defines errors that are safe to expose to API clients.
package apperror

import (
	"context"
	"errors"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/upstream"
)

type Error struct {
	Status  int          `json:"-"`
	Code    string       `json:"code"`
	Message messages.Ref `json:"message"`
	cause   upstream.Cause
}

func (e *Error) Error() string { return e.Code }
func New(status int, code string, message messages.Ref) error {
	return &Error{Status: status, Code: code, Message: message}
}
func (e *Error) DiagnosticCause() upstream.Cause { return e.cause }

// NewWithCause retains only detached diagnostic metadata, never the raw error.
func NewWithCause(status int, code string, message messages.Ref, err error) error {
	return &Error{Status: status, Code: code, Message: message, cause: diagnosticCause(err)}
}
func diagnosticCause(err error) upstream.Cause {
	var own *Error
	if errors.As(err, &own) {
		return own.cause
	}
	return upstream.Classify(err, upstream.Operation)
}
func Public(err error) (result *Error) {
	var own *Error
	if errors.As(err, &own) {
		return own
	}
	defer func() { result.cause = diagnosticCause(err) }()
	code := minio.ToErrorResponse(err).Code
	var adminErr madmin.ErrorResponse
	if errors.As(err, &adminErr) {
		code = adminErr.Code
	}
	switch code {
	case "InvalidAccessKeyId", "SignatureDoesNotMatch":
		return &Error{Status: 403, Code: code, Message: messages.ApperrorCouldNotVerifyTheCredentialsCheckThe}
	case "AccessDenied", "XMinioAdminAccessDenied":
		return &Error{Status: 403, Code: "AccessDenied", Message: messages.ApperrorInsufficientMinIOPermissionsForThisOperation}
	case "NoSuchBucket", "NoSuchKey", "NoSuchUpload", "XMinioAdminNoSuchUser", "XMinioAdminNoSuchGroup", "XMinioAdminNoSuchPolicy", "XMinioAdminNoSuchServiceAccount":
		return &Error{Status: 404, Code: code, Message: messages.ApperrorTheResourceNoLongerExistsRefreshThe}
	case "InvalidBucketState":
		return &Error{Status: 409, Code: code, Message: messages.ApperrorTheBucketStateDoesNotAllowThis}
	case "BucketNotEmpty":
		return &Error{Status: 409, Code: code, Message: messages.ApperrorTheBucketIsNotEmptyThisOperation}
	case "BadDigest", "XAmzContentSHA256Mismatch", "ChecksumMismatch":
		return &Error{Status: 400, Code: "ChecksumMismatch", Message: messages.ApperrorTheStorageRejectedTheDataBecauseThe}
	case "InvalidDigest":
		return &Error{Status: 400, Code: "InvalidChecksum", Message: messages.ApperrorTheStorageRejectedTheChecksumFormatCheck}
	case "BucketAlreadyExists", "BucketAlreadyOwnedByYou", "XMinioAdminGroupNotEmpty", "XMinioAdminPolicyInUse":
		return &Error{Status: 409, Code: code, Message: messages.ApperrorTheResourceAlreadyExistsOrIsIn}
	case "InvalidBucketName", "InvalidArgument", "MalformedPolicy", "XMinioAdminInvalidArgument", "XMinioAdminInvalidAccessKey", "XMinioAdminInvalidSecretKey", "EntityTooLarge":
		return &Error{Status: 400, Code: code, Message: messages.ApperrorMinIORejectedTheParametersCheckTheName}
	case "ExpiredToken", "InvalidToken":
		return &Error{Status: 403, Code: code, Message: messages.ApperrorTheMinIOTokenIsInvalidOrExpired}
	case "NotImplemented", "XMinioAdminNotImplemented":
		return &Error{Status: 501, Code: code, Message: messages.ApperrorThisServerVersionOrTypeDoesNot}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &Error{Status: 504, Code: "Timeout", Message: messages.ApperrorTheS3APIDidNotRespondIn}
	}
	return &Error{Status: 502, Code: "UpstreamError", Message: messages.ApperrorCouldNotCompleteTheMinIORequestCheck}
}

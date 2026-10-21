package iam

import (
	"encoding/json"
	"errors"
	"net"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

func ActionAccountIssue(err error, status int) *ActionCheckIssue {
	issue := ActionReadIssue("AccountInfo", err)
	issue.HTTPStatus = status
	var syntax *json.SyntaxError
	var mismatch *json.UnmarshalTypeError
	var network net.Error
	switch {
	case errors.As(err, &syntax) || errors.As(err, &mismatch):
		issue.Code = "AccountInfoInvalidResponse"
		issue.Cause = "invalid-json"
		issue.Message = messages.IamCouldNotReadMinIOSConnectionOwner
	case apperror.Public(err).Code == "AccountInfoTooLarge":
		issue.Code = "AccountInfoTooLarge"
		issue.Cause = "response-too-large"
		issue.Message = messages.IamTheMinIOPermissionsResponseIsTooLarge
	case issue.Code == "ActionCheckTimeout":
		issue.Cause = "timeout"
	case issue.Code == "AccountInfoDenied":
		issue.Cause = "access-denied"
	case issue.Code == "AccountInfoUnsupported":
		issue.Cause = "unsupported"
	case status == 404 || status == 405:
		issue.Code = "AccountInfoRouteUnavailable"
		issue.Cause = "route-unavailable"
		issue.Message = messages.IamTheMinIOAddressOrProxyDoesNot
	case status >= 500:
		issue.Code = "AccountInfoServerError"
		issue.Cause = "server-error"
		issue.Message = messages.IamMinIOOrItsProxyReturnedAnError
	case errors.As(err, &network):
		issue.Cause = "network"
	default:
		issue.Cause = "upstream"
	}
	return issue
}

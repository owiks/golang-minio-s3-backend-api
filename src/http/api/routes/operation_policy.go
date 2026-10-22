package routes

import (
	"net/http"
	"strings"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/operationpolicy"
)

// OperatorAction describes user-data side effects independently from caller
// scopes. Read-only HTTP operations may use POST; cleanup and local credential
// management must remain available while new upstream changes are suspended.
func OperatorAction(route APIRoute, op Operation) operationpolicy.Action {
	if op.Method == http.MethodGet || op.Method == http.MethodHead {
		return 0
	}
	switch op.Controller {
	case "HandleToken", "HandleLogin", "HandleLogout", "IssueConnectionToken", "EditConnectionToken", "RevokeConnectionToken", "RotateConnectionToken", "RenewConnectionToken", "DeleteConnection", "ReplaceConnectionCredentials":
		return 0
	case "HandleActionAccess", "HandleSelectObject", "HandleContentSearch", "HandleCalculateChecksum", "HandlePrepareArchive", "HandleConfigureArchive", "HandleCancelArchive", "HandleForgetArchive", "HandleCreateFolderShare", "HandleRevokeFolderShare", "HandleRevokeUploadInvite", "HandleAbortMultipart", "HandleAbortIncompleteUpload", "HandleCancelBatchJob":
		return 0
	case "HandleWriteFile", "HandleUpdateObjectContent", "HandleRemoteImport", "HandleReceiveFile", "HandleCreateFolder", "HandleExtractZIPEntry", "HandleFileTransfer", "HandleCreateUploadInvite", "HandleCreateMultipart", "HandleMultipartPart", "HandleSignMultipartPart", "HandleCompleteMultipart":
		return operationpolicy.Upload
	case "HandleDeleteObject", "HandleDeleteFolderObject":
		return operationpolicy.Delete
	case "HandleDeleteBucket":
		return operationpolicy.Admin | operationpolicy.Delete
	}
	for _, scope := range op.Scopes {
		switch scope {
		case "iam:manage", "server:manage", "buckets:manage":
			return operationpolicy.Admin
		}
	}
	// Admin routes classified before scopes are populated retain their gate.
	if strings.HasPrefix(route.Path, "admin/") {
		return operationpolicy.Admin
	}
	// New non-read commands must never silently bypass read-only mode. Named
	// exceptions above are reviewed when adding an operation to the registry.
	return operationpolicy.Write
}

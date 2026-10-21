package runtime

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"

	"github.com/minio/madmin-go/v3"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/iam"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

const MaxActionAccountResponse = 4 * 1024 * 1024

// AccountInfo includes version-dependent statistics and bucket configuration.
// Decode only the identity and policy needed for action hints. Signing and
// credential handling stay in the official SDK; no permissive fallback is used.
func (s *Session) ActionAccountInfo(ctx context.Context) (madmin.AccountInfo, *iam.ActionCheckIssue) {
	wire := &ResponseStatusTransport{Base: s.transport}
	admin, err := s.newAdmin(wire)
	if err != nil {
		return madmin.AccountInfo{}, iam.ActionAccountIssue(err, 0)
	}
	response, err := admin.ExecuteMethod(ctx, http.MethodGet, madmin.RequestData{RelPath: "/v3/accountinfo"})
	status := int(wire.Status.Load())
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil {
		return madmin.AccountInfo{}, iam.ActionAccountIssue(err, status)
	}
	if response == nil || response.Body == nil {
		return madmin.AccountInfo{}, iam.ActionAccountIssue(io.ErrUnexpectedEOF, status)
	}
	if status != http.StatusOK {
		// Ignore server-controlled messages, URLs and identifiers, including HTML
		// proxy responses. Only known error codes influence the public diagnosis.
		var value struct{ Code string }
		body, _ := io.ReadAll(io.LimitReader(response.Body, 100<<10))
		if json.Unmarshal(body, &value) != nil {
			_ = xml.Unmarshal(body, &value)
		}
		if status == http.StatusForbidden && value.Code == "" {
			value.Code = "AccessDenied"
		}
		return madmin.AccountInfo{}, iam.ActionAccountIssue(madmin.ErrorResponse{Code: value.Code}, status)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, MaxActionAccountResponse+1))
	if err != nil {
		return madmin.AccountInfo{}, iam.ActionAccountIssue(err, status)
	}
	if len(raw) > MaxActionAccountResponse {
		return madmin.AccountInfo{}, iam.ActionAccountIssue(apperror.New(502, "AccountInfoTooLarge", messages.RuntimeAccountInfoTooLarge), status)
	}
	var identity struct {
		AccountName string
		Policy      json.RawMessage
	}
	if err = json.Unmarshal(raw, &identity); err != nil {
		return madmin.AccountInfo{}, iam.ActionAccountIssue(err, status)
	}
	return madmin.AccountInfo{AccountName: identity.AccountName, Policy: identity.Policy}, nil
}

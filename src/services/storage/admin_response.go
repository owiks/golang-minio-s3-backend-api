package storage

import (
	"bytes"
	"io"
	"net/http"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

const (
	maxAdminResponse = 8 << 20
	maxAdminHistory  = 4 << 20
	maxAdminError    = 100 << 10
	// The domain still checks 256 KiB of plaintext. This wire allowance also
	// admits the SDK's encryption header and per-block authentication tags.
	maxAdminConfigResponse = (256 << 10) + (4 << 10)
)

// Finite control responses are checked before SDK decryption/decoding. Only
// the two diagnostic subscriptions currently used by this application stream.
// Unknown/new endpoints stay bounded until explicitly reviewed.
func adminResponseLimit(method, path string, status int) int64 {
	if status < 200 || status >= 300 {
		return maxAdminError
	}
	if method == http.MethodHead {
		return 0 // Content-Length describes the corresponding GET, not a body.
	}
	if method == http.MethodGet {
		switch path {
		case "/minio/admin/v3/log", "/minio/admin/v3/trace":
			if status == http.StatusOK {
				return 0
			}
		case "/minio/admin/v3/get-config-kv":
			return maxAdminConfigResponse
		case "/minio/admin/v3/list-config-history-kv":
			return maxAdminHistory
		}
	}
	return maxAdminResponse
}

// Do not return a local body failure as a RoundTrip error: madmin retries
// transport failures, including those caused by oversized read responses.
// Returning the original status with a failing body lets SDK reads terminate
// with the original error. Commands additionally restore it via adminAttempt.
type adminFailedBody struct{ err error }

func (b adminFailedBody) Read([]byte) (int, error) { return 0, b.err }
func (adminFailedBody) Close() error               { return nil }

func boundAdminResponse(method, path string, response *http.Response) error {
	if response == nil || response.Body == nil {
		return nil
	}
	limit := adminResponseLimit(method, path, response.StatusCode)
	if limit == 0 {
		return nil
	}
	body := response.Body
	defer body.Close() // Never drain the untrusted remainder, including in SDK cleanup.
	tooLarge := func() error {
		return apperror.New(502, "AdminResponseTooLarge", messages.StorageAdminResponseTooLarge)
	}
	var data []byte
	var err error
	if response.ContentLength > limit {
		err = tooLarge()
	} else {
		data, err = io.ReadAll(io.LimitReader(body, limit+1))
		if int64(len(data)) > limit {
			err = tooLarge()
		}
	}
	if err != nil {
		response.Body = adminFailedBody{err: err}
		return err
	}
	response.Body = io.NopCloser(bytes.NewReader(data))
	return nil
}

type adminResponseTransport struct{ base http.RoundTripper }

func (t adminResponseTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err == nil {
		if diagnosticResponse(r.Method, r.URL.Path, response) {
			response.Body = newDiagnosticBody(response.Body, func(err error) { failDiagnostic(r.Context(), err) })
		} else {
			_ = boundAdminResponse(r.Method, r.URL.Path, response)
		}
	}
	return response, err
}

func diagnosticResponse(method, path string, response *http.Response) bool {
	return response != nil && response.Body != nil && method == http.MethodGet && response.StatusCode == http.StatusOK && (path == "/minio/admin/v3/log" || path == "/minio/admin/v3/trace")
}

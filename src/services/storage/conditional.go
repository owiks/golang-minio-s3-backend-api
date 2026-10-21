package storage

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
)

// NativeConditionalDelete recognizes AWS S3 API endpoints. Other compatible
// endpoints require an explicit operator assertion after checking their server.
func NativeConditionalDelete(endpoint *url.URL) bool {
	host := strings.ToLower(endpoint.Hostname())
	return endpoint.Scheme == "https" && (host == "s3.amazonaws.com" || (strings.HasPrefix(host, "s3.") || strings.HasPrefix(host, "s3-")) && (strings.HasSuffix(host, ".amazonaws.com") || strings.HasSuffix(host, ".amazonaws.com.cn"))) && !strings.Contains(host, "website")
}

// ConditionalDelete signs the precondition using the official SDK and sends it
// through the session's checked transport. It never follows redirects or retries
// without the precondition, and never targets a version or bypasses retention.
func ConditionalDelete(ctx context.Context, c Config, transport http.RoundTripper, bucket, key, etag string, confirmed bool) error {
	if !confirmed {
		return apperror.New(409, "ConditionalDeleteUnavailable", messages.StorageConditionalDeleteUnavailable)
	}
	if etag == "" || strings.ContainsAny(etag, "\r\n\"") {
		return apperror.New(409, "InvalidDeletionCondition", messages.StorageInvalidDeletionCondition)
	}
	client, err := NewS3(c, transport)
	if err != nil {
		return err
	}
	headers := http.Header{"If-Match": {`"` + etag + `"`}}
	signed, err := client.PresignHeader(ctx, http.MethodDelete, bucket, key, time.Minute, nil, headers)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, signed.String(), nil)
	if err != nil {
		return err
	}
	req.Header = headers
	response, err := (telemetry.Transport{Base: transport}).RoundTrip(req)
	if err != nil {
		return apperror.New(502, "DeleteUncertain", messages.StorageDeleteUncertain)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case 204:
		return nil
	case 409, 412:
		return apperror.New(409, "SourceChanged", messages.StorageSourceChanged)
	case 403:
		return apperror.New(403, "AccessDenied", messages.StorageAccessDenied)
	case 400, 405, 501:
		return apperror.New(409, "ConditionalDeleteUnavailable", messages.StorageConditionalDeleteUnavailableConditional)
	default:
		return apperror.New(502, "DeleteUncertain", messages.StorageDeleteUncertainConditional)
	}
}

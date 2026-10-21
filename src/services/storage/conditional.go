package storage

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/telemetry"
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
		return apperror.New(409, "ConditionalDeleteUnavailable", "Сервер не подтвердил поддержку условного удаления. Копия создана, исходник сохранён.")
	}
	if etag == "" || strings.ContainsAny(etag, "\r\n\"") {
		return apperror.New(409, "InvalidDeletionCondition", "Не удалось получить условие безопасного удаления. Исходник сохранён.")
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
		return apperror.New(502, "DeleteUncertain", "Результат удаления не подтверждён. Проверьте исходник перед повтором.")
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case 204:
		return nil
	case 409, 412:
		return apperror.New(409, "SourceChanged", "Исходник изменился во время перемещения и не удалён.")
	case 403:
		return apperror.New(403, "AccessDenied", "Копия создана, MinIO запретил удаление исходника.")
	case 400, 405, 501:
		return apperror.New(409, "ConditionalDeleteUnavailable", "Сервер отклонил условное удаление. Исходник сохранён.")
	default:
		return apperror.New(502, "DeleteUncertain", "Сервер не подтвердил удаление. Проверьте исходник перед повтором.")
	}
}

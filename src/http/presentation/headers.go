package presentation

import (
	"context"
	"net/http"
	"strings"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/requestctx"
)

// Called for representations containing API messages, including generated ZIP
// manifests, never for stored file metadata.
func LanguageHeaders(ctx context.Context, headers http.Header) {
	headers.Set("Content-Language", requestctx.Language(ctx))
	if !requestctx.AutomaticLanguage(ctx) {
		return
	}
	for _, line := range headers.Values("Vary") {
		for _, field := range strings.Split(line, ",") {
			if strings.EqualFold(strings.TrimSpace(field), "Accept-Language") || strings.TrimSpace(field) == "*" {
				return
			}
		}
	}
	headers.Add("Vary", "Accept-Language")
}

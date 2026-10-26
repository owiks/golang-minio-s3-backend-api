package controllers

import (
	"net/http"
	"net/url"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/requestctx"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

// A native browser download cannot set Accept-Language to the UI preference.
// Override only this generated representation, after the usual authentication.
func archiveDownloadLanguage(r *http.Request) (*http.Request, error) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, apperror.New(400, "InvalidLanguage", messages.OperationInvalid)
	}
	values, present := query["lang"]
	if !present {
		return r, nil
	}
	if len(values) != 1 || values[0] != "en" && values[0] != "ru" {
		return nil, apperror.New(400, "InvalidLanguage", messages.OperationInvalid)
	}
	if !requestctx.AutomaticLanguage(r.Context()) {
		return r, nil
	}
	language := values[0]
	localizer := requestctx.Localizer(r.Context()).ForLanguage(language)
	return r.WithContext(requestctx.WithLanguage(r.Context(), language, true, localizer)), nil
}

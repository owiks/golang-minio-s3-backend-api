package storage

import (
	"bytes"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

// An ordinary object/part PUT has no success payload. The SDK deliberately
// ignores that body, so an upstream's HTTP 200 error document otherwise becomes
// UploadInfo success, even when accompanied by an ETag. Keep copy operations,
// bucket configuration and unknown subresources on their own decoding paths.
func validateS3WriteResult(method string, target *url.URL, requestHeaders, responseHeaders http.Header, status int, data []byte) error {
	if method != http.MethodPut || status != http.StatusOK || requestHeaders.Get("X-Amz-Copy-Source") != "" {
		return nil
	}
	_, key, object := strings.Cut(strings.TrimPrefix(target.Path, "/"), "/")
	if !object || key == "" {
		return nil
	}
	query, err := url.ParseQuery(target.RawQuery)
	if err != nil {
		return nil
	}
	known := len(query) == 0 || len(query) == 1 && len(query["versionId"]) == 1
	if len(query) == 2 && len(query["uploadId"]) == 1 && query.Get("uploadId") != "" && len(query["partNumber"]) == 1 {
		number, err := strconv.Atoi(query.Get("partNumber"))
		known = err == nil && number >= 1 && number <= 10000
	}
	if known && (len(bytes.TrimSpace(data)) != 0 || strings.Trim(strings.TrimSpace(responseHeaders.Get("ETag")), `"`) == "") {
		return apperror.New(502, "S3WriteUnconfirmed", messages.StorageS3WriteUnconfirmed)
	}
	return nil
}

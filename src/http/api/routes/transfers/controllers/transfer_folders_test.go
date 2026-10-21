package controllers

import (
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
)

func TestTransferPreparationDecodesQueryAndPreservesErrors(t *testing.T) {
	for _, parameter := range []string{"version", "versionId"} {
		for _, version := range []string{"", "old-version"} {
			// Presence, including an empty value, means a version was requested.
			q := url.Values{"bucket": {"bucket"}, "prefix": {"folder/"}, parameter: {version}}
			w := httptest.NewRecorder()
			err := HandleTransferList(nil, w, httptest.NewRequest("GET", "/?"+q.Encode(), nil), nil)
			if err == nil || apperror.Public(err).Status != 400 || apperror.Public(err).Code != "InvalidFolderDelete" || w.Body.Len() != 0 {
				t.Fatal("version request reached storage or became success", parameter, version, err)
			}
		}
	}
	for _, tc := range []struct{ path, code string }{
		{"/?bucket=bucket&key=folder%2F..%2Ffile", "InvalidTransfer"},
		{"/?bucket=bucket&key=%2F", "InvalidTransfer"},
		{"/?bucket=bucket", "InvalidObject"},
	} {
		w := httptest.NewRecorder()
		err := HandleTransferPrepare(nil, w, httptest.NewRequest("GET", tc.path, nil), nil)
		if err == nil || apperror.Public(err).Code != tc.code || w.Body.Len() != 0 {
			t.Fatal("destination query validation changed", tc.path, err)
		}
	}
	for _, method := range []string{"POST", "DELETE"} {
		for _, list := range []bool{false, true} {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(method, "/?bucket=bucket&key=file&prefix=folder%2F", nil)
			var err error
			if list {
				err = HandleTransferList(nil, w, r, nil)
			} else {
				err = HandleTransferPrepare(nil, w, r, nil)
			}
			if err == nil || apperror.Public(err).Status != 405 || apperror.Public(err).Code != "MethodNotAllowed" || w.Body.Len() != 0 {
				t.Fatal("unsupported method reached storage", method, list, err)
			}
		}
	}
}

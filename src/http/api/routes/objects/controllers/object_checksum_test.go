package controllers

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/httperrors"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
)

func TestChecksumPreflightErrorsRemainHTTPJSON(t *testing.T) {
	for _, tc := range []struct{ path, body, code string }{
		{"/?bucket=bucket", `{}`, "InvalidObject"},
		{"/?bucket=bucket&key=file.txt", `{"sha256":"bad"}`, "InvalidExpectedChecksum"},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		err := HandleCalculateChecksum(nil, w, r, nil)
		if err == nil || apperror.Public(err).Code != tc.code || w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" {
			t.Fatal("preflight started a stream", err, w.Body.String(), w.Header())
		}
		httperrors.Respond(w, r, err)
		if w.Code != 400 || w.Header().Get("Content-Type") != "application/json; charset=utf-8" || w.Header().Get("X-Accel-Buffering") != "" || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
			t.Fatal("preflight error became NDJSON", w.Code, w.Body.String())
		}
	}
}

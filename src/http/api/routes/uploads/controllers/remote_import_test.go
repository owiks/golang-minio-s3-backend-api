package controllers

import (
	"net/http/httptest"
	"strings"
	"testing"

	"minio-web/api/src/http/httperrors"
	"minio-web/api/src/services/apperror"
)

func TestImportPreflightErrorsRemainHTTPJSON(t *testing.T) {
	for _, tc := range []struct{ path, body, code string }{
		{"/?bucket=bucket", `{}`, "InvalidObject"},
		{"/?bucket=bucket&key=file.txt", `{"url":"file:///private","revision":"absent"}`, "InvalidSourceURL"},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		err := HandleImportFromURL(nil, w, r, nil, nil)
		if err == nil || apperror.Public(err).Code != tc.code || w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" {
			t.Fatal("preflight started a stream", err, w.Body.String(), w.Header())
		}
		httperrors.Respond(w, err)
		if w.Code != 400 || w.Header().Get("Content-Type") != "application/json; charset=utf-8" || w.Header().Get("X-Accel-Buffering") != "" || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
			t.Fatal("preflight error became NDJSON", w.Code, w.Body.String())
		}
	}
}

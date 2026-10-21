package controllers

import (
	"net/http/httptest"
	"strings"
	"testing"

	"minio-web/api/src/config"
	"minio-web/api/src/http/httperrors"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/runtime"
	"minio-web/api/src/services/selectquery"
	"minio-web/api/tests/testsession"
)

func TestSelectPreflightErrorsDoNotStartNDJSON(t *testing.T) {
	app := runtime.New(config.Default())
	defer app.Close()
	session := testsession.New(t, "http://fixture.invalid")
	for _, tc := range []struct{ name, path, body, code string }{
		{"bad expression", "/?bucket=bucket&key=file.csv", `{"expression":"","input":{"csv":{}},"output":{"json":{}}}`, "InvalidSelectQuery"},
		{"missing object", "/?bucket=bucket", `{"expression":"SELECT * FROM S3Object","input":{"csv":{}},"output":{"json":{}}}`, "InvalidObject"},
		{"version", "/?bucket=bucket&key=file.csv&versionId=v", `{}`, "InvalidSelectQuery"},
		{"unknown body field", "/?bucket=bucket&key=file.csv", `{"expression":"SELECT * FROM S3Object","input":{"csv":{}},"output":{"json":{}},"url":"http://other"}`, "InvalidJSON"},
		{"body limit", "/?bucket=bucket&key=file.csv", strings.Repeat(" ", selectquery.MaxRequestBytes+1), "InvalidJSON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			err := HandleSelectObject(app, w, r, session)
			if err == nil || apperror.Public(err).Code != tc.code || w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" {
				t.Fatal(err, w.Body.String(), w.Header())
			}
			httperrors.Respond(w, r, err)
			if w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
				t.Fatal("preflight became stream", w.Header())
			}
		})
	}
}

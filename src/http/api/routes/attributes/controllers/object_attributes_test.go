package controllers

import (
	"net/http/httptest"
	"testing"

	"minio-web/api/src/services/apperror"
)

func TestAttributesInvalidQueryStopsBeforeClient(t *testing.T) {
	for _, query := range []string{
		"bucket=bucket&key=file&maxParts=bad",
		"bucket=bucket&key=file&maxParts=0",
		"bucket=bucket&key=file&maxParts=1001",
		"bucket=bucket&key=file&partNumberMarker=-1",
		"bucket=bucket&key=file&partNumberMarker=1",
		"bucket=bucket&key=file&snapshot=bad",
		"bucket=bucket&key=file&versionId=bad%0A",
	} {
		t.Run(query, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://ui.example/objects/attributes?"+query, nil)
			err := HandleGetObjectAttributes(nil, httptest.NewRecorder(), r, nil)
			if err == nil || apperror.Public(err).Status != 400 {
				t.Fatal("invalid query reached SDK", err)
			}
		})
	}
}

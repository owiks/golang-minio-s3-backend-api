package apperror

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"minio-web/api/src/services/upstream"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
)

func TestErrorsNeverExposeUpstreamDetails(t *testing.T) {
	for _, err := range []error{&Error{Status: 400, Code: "Invalid", Message: "safe"}, madmin.ErrorResponse{Code: "AccessDenied", Message: "SECRET-TOKEN"}, minio.ErrorResponse{Code: "SignatureDoesNotMatch", Message: "SECRET-TOKEN"}, io.ErrUnexpectedEOF} {
		data, _ := json.Marshal(Public(err))
		if bytes.Contains(data, []byte("SECRET-TOKEN")) {
			t.Fatal("leaked upstream message")
		}
	}
}

func TestDiagnosticCauseSurvivesMappingWithoutChangingPublicContract(t *testing.T) {
	raw := madmin.ErrorResponse{Code: "PRIVATE-CODE", Message: "PRIVATE-MESSAGE"}
	problem := Public(raw)
	if problem.Status != 502 || problem.Code != "UpstreamError" || problem.DiagnosticCause() != (upstream.Cause{Class: "sdk", Stage: upstream.Operation}) {
		t.Fatal("public mapping or safe cause changed", problem.DiagnosticCause())
	}
	wrapped := NewWithCause(502, "ServerOperationFailed", "safe", problem)
	value := Public(wrapped)
	if value.DiagnosticCause() != problem.DiagnosticCause() || Public(value) != value {
		t.Fatal("mapping lost detached cause or changed own error identity")
	}
	cause := value.DiagnosticCause()
	cause.Class = "PRIVATE"
	if value.DiagnosticCause().Class != "sdk" {
		t.Fatal("diagnostic state exposed")
	}
	encoded, err := json.Marshal(value)
	if err != nil || string(encoded) != `{"code":"ServerOperationFailed","message":"safe"}` {
		t.Fatal("public JSON changed", string(encoded), err)
	}
	unknown := Public(errors.New("PRIVATE-ERROR"))
	if unknown.DiagnosticCause().Class != "unknown" {
		t.Fatal("unverified error guessed as SDK/network failure")
	}
	if Public(New(400, "InvalidName", "safe")).DiagnosticCause().Valid() {
		t.Fatal("local validation has upstream cause")
	}
}

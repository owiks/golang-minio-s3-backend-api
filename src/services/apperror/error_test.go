package apperror

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/upstream"
	"minio-web/api/tests/testmessage"
)

func TestErrorsNeverExposeUpstreamDetails(t *testing.T) {
	for _, err := range []error{&Error{Status: 400, Code: "Invalid", Message: messages.Literal("safe")}, madmin.ErrorResponse{Code: "AccessDenied", Message: "SECRET-TOKEN"}, minio.ErrorResponse{Code: "SignatureDoesNotMatch", Message: "SECRET-TOKEN"}, io.ErrUnexpectedEOF} {
		data, _ := json.Marshal(testmessage.Public(Public(err).Code, Public(err).Message))
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
	wrapped := NewWithCause(502, "ServerOperationFailed", messages.Literal("safe"), problem)
	value := Public(wrapped)
	if value.DiagnosticCause() != problem.DiagnosticCause() || Public(value) != value {
		t.Fatal("mapping lost detached cause or changed own error identity")
	}
	cause := value.DiagnosticCause()
	cause.Class = "PRIVATE"
	if value.DiagnosticCause().Class != "sdk" {
		t.Fatal("diagnostic state exposed")
	}
	encoded, err := json.Marshal(testmessage.Public(value.Code, value.Message))
	if err != nil || string(encoded) != `{"code":"ServerOperationFailed","message":"safe"}` {
		t.Fatal("public JSON changed", string(encoded), err)
	}
	unknown := Public(errors.New("PRIVATE-ERROR"))
	if unknown.DiagnosticCause().Class != "unknown" {
		t.Fatal("unverified error guessed as SDK/network failure")
	}
	if Public(New(400, "InvalidName", messages.Literal("safe"))).DiagnosticCause().Valid() {
		t.Fatal("local validation has upstream cause")
	}
}

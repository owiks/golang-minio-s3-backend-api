package upstream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"syscall"
	"testing"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
)

// The classifier must never inspect Error(), including for unknown failures.
type opaqueFailure struct{}

func (opaqueFailure) Error() string { panic("raw error text inspected") }

func TestTypedClassificationNeverInspectsErrorText(t *testing.T) {
	for _, tc := range []struct {
		name, class string
		err         error
		stage       Stage
		timeout     bool
	}{
		{"cancelled", "cancelled", fmt.Errorf("PRIVATE: %w", context.Canceled), Request, false},
		{"deadline", "timeout", context.DeadlineExceeded, Request, true},
		{"wrapped DNS", "dns", &url.Error{Op: "GET", URL: "https://PRIVATE/?token=SECRET", Err: &net.DNSError{Name: "PRIVATE", Err: "SECRET"}}, Request, false},
		{"DNS timeout", "dns", &net.DNSError{IsTimeout: true, Name: "PRIVATE"}, Request, true},
		{"TLS verification", "tls", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, Request, false},
		{"TLS authority", "tls", x509.UnknownAuthorityError{}, Request, false},
		{"TLS hostname", "tls", x509.HostnameError{}, Request, false},
		{"TLS invalid cert", "tls", x509.CertificateInvalidError{}, Request, false},
		{"TLS roots", "tls", x509.SystemRootsError{}, Request, false},
		{"TLS record", "tls", tls.RecordHeaderError{Msg: "PRIVATE"}, Request, false},
		{"TLS alert", "tls", tls.AlertError(40), Request, false},
		{"connect", "connect", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, Request, false},
		{"unreachable", "connect", fmt.Errorf("PRIVATE: %w", syscall.EHOSTUNREACH), Request, false},
		{"read reset", "read", &net.OpError{Op: "read", Err: syscall.ECONNRESET}, Request, false},
		{"unexpected EOF", "read", io.ErrUnexpectedEOF, Request, false},
		{"body timeout", "timeout", context.DeadlineExceeded, Body, true},
		{"body opaque failure", "read", opaqueFailure{}, Body, false},
		{"transport opaque failure", "transport", opaqueFailure{}, Request, false},
		{"unverified cause", "unknown", opaqueFailure{}, Operation, false},
		{"no text heuristics", "unknown", errors.New("dial DNS tls timeout PRIVATE"), Operation, false},
		{"SDK decode failure", "sdk", opaqueFailure{}, SDK, false},
		{"SDK S3 error", "sdk", minio.ErrorResponse{Code: "PRIVATE", Message: "SECRET"}, Operation, false},
		{"SDK S3 pointer", "sdk", &minio.ErrorResponse{Code: "PRIVATE"}, Operation, false},
		{"SDK admin error", "sdk", madmin.ErrorResponse{Code: "PRIVATE", Message: "SECRET"}, Operation, false},
		{"SDK admin pointer", "sdk", &madmin.ErrorResponse{Code: "PRIVATE"}, Operation, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := Classify(tc.err, tc.stage)
			if !cause.Valid() || cause.Class != tc.class || cause.Stage != tc.stage || cause.Timeout != tc.timeout {
				t.Fatalf("classification=%+v", cause)
			}
		})
	}
	if Classify(nil, Operation) != (Cause{}) || (Cause{Class: "PRIVATE", Stage: SDK}).Valid() || (Cause{Class: "sdk", Stage: "SECRET"}).Valid() {
		t.Fatal("invalid metadata accepted")
	}
}

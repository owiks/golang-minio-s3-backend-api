// Package upstream classifies failures without retaining error text or resource data.
package upstream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"syscall"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
)

type Stage string

const (
	Request   Stage = "request"
	Headers   Stage = "headers"
	Body      Stage = "body"
	SDK       Stage = "sdk"
	Operation Stage = "operation"
)

// Cause is a detached, bounded diagnostic value, never the original error.
type Cause struct {
	Class   string
	Stage   Stage
	Timeout bool
}

func (c Cause) Valid() bool {
	switch c.Stage {
	case Request, Headers, Body, SDK, Operation:
	default:
		return false
	}
	switch c.Class {
	case "dns", "tls", "connect", "timeout", "read", "sdk", "cancelled", "transport", "http", "unknown":
		return true
	}
	return false
}

func Classify(err error, stage Stage) Cause {
	if err == nil {
		return Cause{}
	}
	var detached interface{ DiagnosticCause() Cause }
	if errors.As(err, &detached) {
		if cause := detached.DiagnosticCause(); cause.Valid() {
			cause.Stage = stage
			return cause
		}
	}
	c := Cause{Stage: stage}
	var timed net.Error
	c.Timeout = errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timed) && timed.Timeout()
	var dns *net.DNSError
	var verify *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	var roots x509.SystemRootsError
	var record tls.RecordHeaderError
	var alert tls.AlertError
	var op *net.OpError
	var s3 minio.ErrorResponse
	var admin madmin.ErrorResponse
	var s3ptr *minio.ErrorResponse
	var adminptr *madmin.ErrorResponse
	switch {
	case errors.Is(err, context.Canceled):
		c.Class = "cancelled"
	case errors.As(err, &dns):
		c.Class = "dns"
	case errors.As(err, &verify), errors.As(err, &authority), errors.As(err, &invalid), errors.As(err, &hostname), errors.As(err, &roots), errors.As(err, &record), errors.As(err, &alert):
		c.Class = "tls"
	case c.Timeout:
		c.Class = "timeout"
	case errors.As(err, &op) && (op.Op == "dial" || op.Op == "connect"), errors.Is(err, syscall.ECONNREFUSED), errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH):
		c.Class = "connect"
	case stage == Body, errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF), op != nil && op.Op == "read":
		c.Class = "read"
	case errors.As(err, &s3), errors.As(err, &admin), errors.As(err, &s3ptr), errors.As(err, &adminptr), stage == SDK:
		c.Class = "sdk"
	case stage == Request:
		c.Class = "transport"
	default:
		c.Class = "unknown"
	}
	return c
}

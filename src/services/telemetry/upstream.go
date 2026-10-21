package telemetry

import (
	"context"
	"errors"
	"time"

	"minio-web/api/src/services/upstream"
)

// UpstreamSnapshot is a value snapshot, not a list of URLs or SDK error codes.
// Attempts count calls to the underlying transport, not confirmed network sends.
type UpstreamSnapshot struct {
	Attempts       int
	Status         int
	Failures       int
	FailureClass   string
	FailureStage   upstream.Stage
	FailureStatus  int
	FailureAttempt int
	FailureTimeout bool
}

func (t *Trace) UpstreamSnapshot() UpstreamSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.upstream
}

func beginUpstream(ctx context.Context) int {
	if t := From(ctx); t != nil {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.UpstreamCalls++
		t.upstream.Attempts = t.UpstreamCalls
		return t.UpstreamCalls
	}
	return 0
}

func finishUpstream(ctx context.Context, attempt, status int, elapsed time.Duration, err error) {
	if t := From(ctx); t != nil {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.UpstreamTime += elapsed
		status = safeStatus(status)
		if attempt >= t.statusAttempt {
			t.upstream.Status, t.statusAttempt = status, attempt
		}
		if err != nil {
			t.upstreamFailure(upstream.Classify(err, upstream.Request), status, attempt)
		} else if status >= 400 {
			t.upstreamFailure(upstream.Cause{Class: "http", Stage: upstream.Headers}, status, attempt)
		}
	}
}

func safeStatus(status int) int {
	if status >= 100 && status <= 599 {
		return status
	}
	return 0
}

func (t *Trace) upstreamFailure(cause upstream.Cause, status, attempt int) {
	if !cause.Valid() {
		return
	}
	t.upstream.Failures++
	t.upstream.FailureClass = cause.Class
	t.upstream.FailureStage = cause.Stage
	t.upstream.FailureStatus = safeStatus(status)
	t.upstream.FailureAttempt = attempt
	t.upstream.FailureTimeout = cause.Timeout
}

func bodyFailure(ctx context.Context, attempt, status int, err error) {
	if t := From(ctx); t != nil {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.upstreamFailure(upstream.Classify(err, upstream.Body), status, attempt)
	}
}

// Error observes a raw or already sanitized failure before the HTTP envelope.
// Local public validation errors carry no upstream cause and are ignored.
func Error(ctx context.Context, err error) {
	if err == nil {
		return
	}
	var detached interface{ DiagnosticCause() upstream.Cause }
	var cause upstream.Cause
	if errors.As(err, &detached) {
		cause = detached.DiagnosticCause()
	} else {
		cause = upstream.Classify(err, upstream.Operation)
	}
	recordOperationFailure(ctx, cause)
}

// SDKError is used only at a trusted SDK boundary, including decoding failures
// and local SDK validation. More specific wire/body evidence takes precedence.
func SDKError(ctx context.Context, err error) {
	if err != nil {
		recordOperationFailure(ctx, upstream.Classify(err, upstream.SDK))
	}
}

func recordOperationFailure(ctx context.Context, cause upstream.Cause) {
	if !cause.Valid() {
		return
	}
	if t := From(ctx); t != nil {
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.upstream.Failures != 0 {
			if t.upstream.FailureStage == upstream.Request || t.upstream.FailureStage == upstream.Body || t.upstream.FailureAttempt == t.UpstreamCalls && t.upstream.FailureAttempt != 0 {
				return // Original wire/body evidence takes precedence over fallback.
			}
		}
		if t.operationFaultSeen && t.operationFaultCalls == t.UpstreamCalls {
			return // Error and its public envelope are the same observation.
		}
		// A mapped/SDK error has no proven association with a particular wire
		// attempt, especially with parallel SDK calls. Do not invent one.
		t.upstreamFailure(cause, 0, 0)
		t.operationFaultCalls, t.operationFaultSeen = t.UpstreamCalls, true
	}
}

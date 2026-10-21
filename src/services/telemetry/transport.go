package telemetry

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

type Transport struct{ Base http.RoundTripper }

// RoundTrip measures time to upstream response headers. Streaming body transfer
// is accounted for by the enclosing API request, not this header latency.
func (t Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	attempt := beginUpstream(r.Context())
	started := time.Now()
	response, err := t.Base.RoundTrip(r)
	status := 0
	if response != nil {
		status = response.StatusCode
	}
	finishUpstream(r.Context(), attempt, status, time.Since(started), err)
	if attempt != 0 && response != nil && response.Body != nil {
		expected := response.ContentLength
		if r.Method == http.MethodHead || response.StatusCode == 204 || response.StatusCode == 304 {
			expected = -1
		}
		response.Body = &responseBody{ReadCloser: response.Body, ctx: r.Context(), attempt: attempt, status: status, expected: expected}
	}
	return response, err
}

type responseBody struct {
	io.ReadCloser
	ctx             context.Context
	attempt, status int
	expected, read  int64
	once            sync.Once
}

func (b *responseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.read += int64(n)
	cause := err
	if err == io.EOF {
		cause = nil
		if b.expected >= 0 && b.read < b.expected {
			cause = io.ErrUnexpectedEOF
		}
	}
	if cause != nil {
		b.once.Do(func() { bodyFailure(b.ctx, b.attempt, b.status, cause) })
	}
	return n, err // Diagnostics never turn a partial read into success or retry.
}

package storage

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/minio/madmin-go/v3"
)

type diagnosticKey struct{}

type diagnosticScope struct {
	mu      sync.Mutex
	failure error
	cancel  context.CancelCauseFunc
	status  atomic.Int32
}

func (s *diagnosticScope) err() error { s.mu.Lock(); defer s.mu.Unlock(); return s.failure }
func failDiagnostic(ctx context.Context, err error) {
	// An interval ending normally cancels its native HTTP read. It is not a
	// malformed/oversized record and must remain a successful timed collection.
	if ctx.Err() != nil {
		return
	}
	s, _ := ctx.Value(diagnosticKey{}).(*diagnosticScope)
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.failure == nil {
		s.failure = err
	}
	s.mu.Unlock()
	s.cancel(err) // Stop the SDK's implicit stream reconnect after a decode error.
}

type diagnosticStatusTransport struct {
	base  http.RoundTripper
	scope *diagnosticScope
}

func (t diagnosticStatusTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// The SDK retry timer can yield its immediate first attempt even after
	// cancellation. Do not invoke the underlying transport on that path.
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	if err := t.scope.err(); err != nil {
		return nil, err
	}
	response, err := t.base.RoundTrip(r)
	if response != nil {
		t.scope.status.Store(int32(response.StatusCode))
	}
	return response, err
}

func diagnosticClient(parent context.Context, c Config, transport http.RoundTripper) (context.Context, *AdminClient, *diagnosticScope, error) {
	ctx, cancel := context.WithCancelCause(parent)
	s := &diagnosticScope{cancel: cancel}
	ctx = context.WithValue(ctx, diagnosticKey{}, s)
	if transport == nil {
		transport = madmin.DefaultTransport(c.Endpoint.Scheme == "https")
	}
	admin, err := NewAdmin(c, diagnosticStatusTransport{base: transport, scope: s})
	if err != nil {
		cancel(err)
	}
	return ctx, admin, s, err
}

func OpenDiagnosticTrace(parent context.Context, c Config, transport http.RoundTripper, onlyErrors bool) (DiagnosticStream[madmin.ServiceTraceInfo], error) {
	ctx, admin, scope, err := diagnosticClient(parent, c, transport)
	if err != nil {
		return nil, err
	}
	events := admin.ServiceTrace(ctx, madmin.ServiceTraceOpts{S3: true, OnlyErrors: onlyErrors})
	return newDiagnosticStream(events, func() { scope.cancel(context.Canceled) }, func() int { return int(scope.status.Load()) }, scope.err), nil
}

func OpenDiagnosticLogs(parent context.Context, c Config, transport http.RoundTripper, limit int) (DiagnosticStream[madmin.LogInfo], error) {
	ctx, admin, scope, err := diagnosticClient(parent, c, transport)
	if err != nil {
		return nil, err
	}
	// Preserve native ALL; filtering ERROR/FATAL belongs to the collection service.
	events := admin.GetLogs(ctx, "", limit, "ALL")
	return newDiagnosticStream(events, func() { scope.cancel(context.Canceled) }, func() int { return int(scope.status.Load()) }, scope.err), nil
}

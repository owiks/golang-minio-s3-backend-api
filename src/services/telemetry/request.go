// Package telemetry records bounded metadata, never URLs, headers, credentials,
// request bodies, upstream errors or signed links.
package telemetry

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
	"time"
)

type contextKey struct{}
type Trace struct {
	mu                  sync.Mutex
	ID, Operation, Code string
	Result              string
	ActorID, ServerID   string
	UpstreamCalls       int
	UpstreamTime        time.Duration
	MutationWait        time.Duration
	upstream            UpstreamSnapshot
	statusAttempt       int
	operationFaultCalls int
	operationFaultSeen  bool
}

func Start(ctx context.Context, operation string) (context.Context, *Trace) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic("secure random unavailable")
	}
	trace := &Trace{ID: hex.EncodeToString(id[:]), Operation: operation}
	return context.WithValue(ctx, contextKey{}, trace), trace
}
func From(ctx context.Context) *Trace { value, _ := ctx.Value(contextKey{}).(*Trace); return value }
func Failure(ctx context.Context, code string) {
	if t := From(ctx); t != nil {
		t.mu.Lock()
		t.Code = code
		if t.Result == "" || t.Result == "success" {
			t.Result = "error"
		}
		t.mu.Unlock()
	}
}
func (t *Trace) Log(ctx context.Context, status int, bytes int64, elapsed time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := t.Result
	if result == "" {
		if errors.Is(ctx.Err(), context.Canceled) {
			result = "cancelled"
		} else if status >= 400 {
			result = "error"
		} else {
			result = "success"
		}
	}
	level := slog.LevelInfo
	if result == "cancelled" {
		level = slog.LevelInfo
	} else if status >= 500 || result == "error" && status < 400 {
		level = slog.LevelError
	} else if status >= 400 || result == "partial" {
		level = slog.LevelWarn
	}
	u := t.upstream
	slog.Log(ctx, level, "api request", "request_id", t.ID, "operation", t.Operation, "status", status, "result", result, "duration_ms", float64(elapsed.Microseconds())/1000, "response_bytes", bytes, "mutation_wait_ms", float64(t.MutationWait.Microseconds())/1000, "error_code", t.Code, "client_cancelled", errors.Is(ctx.Err(), context.Canceled), "actor_id", t.ActorID, "server_id", t.ServerID, "upstream_calls", t.UpstreamCalls, "upstream_ms", float64(t.UpstreamTime.Microseconds())/1000,
		"upstream_status", u.Status, "upstream_failures", u.Failures, "upstream_failure_class", u.FailureClass, "upstream_failure_stage", u.FailureStage, "upstream_failure_status", u.FailureStatus, "upstream_failure_attempt", u.FailureAttempt, "upstream_failure_timeout", u.FailureTimeout)
}

// Identify hashes identifiers so neither access key IDs nor endpoint names enter logs.
func Identify(ctx context.Context, actor, server string) {
	if t := From(ctx); t != nil {
		a, b := sha256.Sum256([]byte(actor)), sha256.Sum256([]byte(server))
		t.mu.Lock()
		t.ActorID = hex.EncodeToString(a[:])
		t.ServerID = hex.EncodeToString(b[:])
		t.mu.Unlock()
	}
}
func Event(ctx context.Context, event string) {
	if t := From(ctx); t != nil {
		t.mu.Lock()
		defer t.mu.Unlock()
		slog.InfoContext(ctx, event, "request_id", t.ID, "operation", t.Operation, "actor_id", t.ActorID, "server_id", t.ServerID)
	}
}

func Waited(ctx context.Context, duration time.Duration) {
	if t := From(ctx); t != nil {
		t.mu.Lock()
		t.MutationWait += duration
		t.mu.Unlock()
	}
}

// Outcome records the domain result independently of a streaming HTTP status.
func Outcome(ctx context.Context, result, code string) {
	switch result {
	case "success", "partial", "cancelled", "error":
	default:
		return
	}
	if t := From(ctx); t != nil {
		t.mu.Lock()
		t.Result = result
		if code != "" {
			t.Code = code
		}
		t.mu.Unlock()
	}
}
func (t *Trace) ResultName(ctx context.Context, status int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.Result != "" {
		return t.Result
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled"
	}
	if status >= 400 {
		return "error"
	}
	return "success"
}

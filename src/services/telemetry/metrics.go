package telemetry

import (
	"log/slog"
	"sync"
	"time"
)

type Counts struct {
	Requests  uint64
	Errors    uint64
	Partial   uint64
	Cancelled uint64
	Duration  time.Duration
}

// Operations are static route templates, never user-controlled resource names.
type Metrics struct {
	mu         sync.Mutex
	active     int
	operations map[string]Counts
}

func (m *Metrics) Begin() { m.mu.Lock(); m.active++; m.mu.Unlock() }
func (m *Metrics) End(operation string, status int, duration time.Duration, results ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active--
	if m.operations == nil {
		m.operations = map[string]Counts{}
	}
	c := m.operations[operation]
	c.Requests++
	result := ""
	if len(results) > 0 {
		result = results[0]
	}
	if result == "partial" {
		c.Partial++
	}
	if result == "cancelled" {
		c.Cancelled++
	}
	if result != "cancelled" && (status >= 400 || result == "error" || result == "partial") {
		c.Errors++
	}
	c.Duration += duration
	m.operations[operation] = c
}

// LogInterval drains counters; no unbounded history or public metrics endpoint.
func (m *Metrics) LogInterval(logger *slog.Logger) {
	m.mu.Lock()
	counters := m.operations
	active := m.active
	m.operations = nil
	m.mu.Unlock()
	for operation, c := range counters {
		if operation == "GET /health" || operation == "GET /ready" || operation == "documentation" {
			continue
		}
		logger.Info("api interval", "operation", operation, "requests", c.Requests, "errors", c.Errors, "partial", c.Partial, "cancelled", c.Cancelled, "duration_ms", c.Duration.Milliseconds(), "active_requests", active)
	}
}

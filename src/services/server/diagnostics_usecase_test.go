package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/storage"
)

type diagnosticFixtureStream[T any] struct {
	events chan T
	status int
	closes atomic.Int32
}

func (f *diagnosticFixtureStream[T]) Events() <-chan T { return f.events }
func (f *diagnosticFixtureStream[T]) Status() int      { return f.status }
func (f *diagnosticFixtureStream[T]) Err() error       { return nil }
func (f *diagnosticFixtureStream[T]) Close()           { f.closes.Add(1) }

type diagnosticFixture struct {
	trace                   *diagnosticFixtureStream[madmin.ServiceTraceInfo]
	logs                    *diagnosticFixtureStream[madmin.LogInfo]
	err                     error
	traces, logCalls, limit int
	onlyErrors              bool
	ctx                     context.Context
	opened                  func(context.Context)
}

func (f *diagnosticFixture) OpenDiagnosticTrace(ctx context.Context, onlyErrors bool) (storage.DiagnosticStream[madmin.ServiceTraceInfo], error) {
	f.ctx, f.onlyErrors = ctx, onlyErrors
	f.traces++
	if f.opened != nil {
		f.opened(ctx)
	}
	return f.trace, f.err
}
func (f *diagnosticFixture) OpenDiagnosticLogs(ctx context.Context, limit int) (storage.DiagnosticStream[madmin.LogInfo], error) {
	f.ctx, f.limit = ctx, limit
	f.logCalls++
	if f.opened != nil {
		f.opened(ctx)
	}
	return f.logs, f.err
}
func fixtureLog(t *testing.T, raw string) madmin.LogInfo {
	t.Helper()
	var value madmin.LogInfo
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestReadDiagnosticsValidationDoesNotOpenSubscription(t *testing.T) {
	valid := DiagnosticOptions{Kind: "trace", Level: "ALL", Seconds: 1, Limit: 1}
	for _, change := range []func(*DiagnosticOptions){
		func(o *DiagnosticOptions) { o.Kind = "other" }, func(o *DiagnosticOptions) { o.Level = "DEBUG" },
		func(o *DiagnosticOptions) { o.Seconds = 0 }, func(o *DiagnosticOptions) { o.Seconds = 11 },
		func(o *DiagnosticOptions) { o.Limit = 0 }, func(o *DiagnosticOptions) { o.Limit = 201 },
	} {
		options := valid
		change(&options)
		if _, err := ReadDiagnostics(context.Background(), nil, options); err == nil || apperror.Public(err).Code != "InvalidDiagnostic" {
			t.Fatal(options, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadDiagnostics(ctx, nil, valid); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestReadDiagnosticTraceLimitAndPrivacy(t *testing.T) {
	stream := &diagnosticFixtureStream[madmin.ServiceTraceInfo]{events: make(chan madmin.ServiceTraceInfo, 2), status: 200}
	stream.events <- madmin.ServiceTraceInfo{Trace: madmin.TraceInfo{FuncName: "s3.GetObject", NodeName: "node:9000", HTTP: &madmin.TraceHTTPStats{}}}
	stream.events <- madmin.ServiceTraceInfo{Trace: madmin.TraceInfo{FuncName: "s3.PutObject", Path: "https://user:SECRET@host/file?token=SECRET", NodeName: "SECRET/unsafe"}}
	source := &diagnosticFixture{trace: stream}
	report, err := ReadDiagnostics(context.Background(), source, DiagnosticOptions{Kind: "trace", Level: "ERROR", Seconds: 10, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rows) != 2 || !report.Limited || source.traces != 1 || source.logCalls != 0 || !source.onlyErrors || stream.closes.Load() != 1 {
		t.Fatalf("report=%+v source=%+v", report, source)
	}
	if source.ctx.Err() != context.Canceled || report.Started.IsZero() || report.Ended.Before(report.Started) {
		t.Fatal("subscription or report lifetime lost")
	}
	first := report.Rows[0]
	if first.Input == nil || *first.Input != 0 || first.Output == nil || *first.Output != 0 || first.Duration == nil {
		t.Fatal("zero metrics omitted", first)
	}
	raw, _ := json.Marshal(report)
	if strings.Contains(string(raw), "SECRET") || report.Rows[1].Path != "/file" {
		t.Fatal("raw upstream data escaped whitelist", string(raw))
	}
}

func TestReadDiagnosticLogsFiltersBeforeCounting(t *testing.T) {
	stream := &diagnosticFixtureStream[madmin.LogInfo]{events: make(chan madmin.LogInfo, 4), status: 200}
	for _, raw := range []string{
		`{"level":"INFO","message":"SECRET"}`,
		`{"level":"error","node":"node:9000","api":{"name":"GetObject"},"message":"SECRET","requestHeader":{"Authorization":"SECRET"}}`,
		`{"level":"DEBUG","message":"SECRET"}`,
		`{"level":"FATAL","node":"node:9000","api":{"name":"PutObject"},"message":"SECRET"}`,
	} {
		stream.events <- fixtureLog(t, raw)
	}
	source := &diagnosticFixture{logs: stream}
	report, err := ReadDiagnostics(context.Background(), source, DiagnosticOptions{Kind: "logs", Level: "ERROR", Seconds: 1, Limit: 2})
	if err != nil || len(report.Rows) != 2 || !report.Limited || source.logCalls != 1 || source.traces != 0 || source.limit != 2 || stream.closes.Load() != 1 {
		t.Fatal(report, err, source)
	}
	raw, _ := json.Marshal(report)
	if strings.Contains(string(raw), "SECRET") || report.Rows[0].API != "GetObject" || report.Rows[1].Level != "FATAL" {
		t.Fatal(string(raw))
	}
	if report.Rows[0].Duration != nil || report.Rows[0].Input != nil || report.Rows[0].Output != nil {
		t.Fatal("invented log metrics")
	}
}

func TestReadDiagnosticsEmptyIntervalIsSuccessful(t *testing.T) {
	for _, kind := range []string{"trace", "logs"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			source := &diagnosticFixture{trace: &diagnosticFixtureStream[madmin.ServiceTraceInfo]{events: make(chan madmin.ServiceTraceInfo), status: 200}, logs: &diagnosticFixtureStream[madmin.LogInfo]{events: make(chan madmin.LogInfo), status: 200}}
			report, err := ReadDiagnostics(context.Background(), source, DiagnosticOptions{Kind: kind, Level: "ALL", Seconds: 1, Limit: 1})
			if err != nil || report.Rows == nil || len(report.Rows) != 0 || report.Limited || time.Since(report.Started) < time.Second {
				t.Fatal(report, err)
			}
			if source.ctx.Err() != context.DeadlineExceeded {
				t.Fatal(source.ctx.Err())
			}
		})
	}
}

func TestReadDiagnosticsFailuresAndParentCancellation(t *testing.T) {
	for _, kind := range []string{"trace", "logs"} {
		for _, status := range []int{0, 200, 403, 404, 501} {
			source := &diagnosticFixture{trace: &diagnosticFixtureStream[madmin.ServiceTraceInfo]{events: make(chan madmin.ServiceTraceInfo), status: status}, logs: &diagnosticFixtureStream[madmin.LogInfo]{events: make(chan madmin.LogInfo), status: status}}
			close(source.trace.events)
			close(source.logs.events)
			_, err := ReadDiagnostics(context.Background(), source, DiagnosticOptions{Kind: kind, Level: "ALL", Seconds: 1, Limit: 1})
			if err == nil {
				t.Fatal("premature close reported as success", kind, status)
			}
			want := "DiagnosticStreamClosed"
			if status == 403 {
				want = "AccessDenied"
			}
			if status == 404 || status == 501 {
				want = "NotImplemented"
			}
			if apperror.Public(err).Code != want {
				t.Fatal(kind, status, err)
			}
		}
		openError := errors.New("SECRET-connection-failure")
		source := &diagnosticFixture{err: openError}
		if _, err := ReadDiagnostics(context.Background(), source, DiagnosticOptions{Kind: kind, Level: "ALL", Seconds: 1, Limit: 1}); !errors.Is(err, openError) {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		source = &diagnosticFixture{trace: &diagnosticFixtureStream[madmin.ServiceTraceInfo]{events: make(chan madmin.ServiceTraceInfo)}, logs: &diagnosticFixtureStream[madmin.LogInfo]{events: make(chan madmin.LogInfo)}, opened: func(context.Context) { cancel() }}
		if _, err := ReadDiagnostics(ctx, source, DiagnosticOptions{Kind: kind, Level: "ALL", Seconds: 1, Limit: 1}); !errors.Is(err, context.Canceled) {
			t.Fatal("parent cancellation masked", kind, err)
		}
		if source.trace.closes.Load()+source.logs.closes.Load() != 1 {
			t.Fatal("cancelled stream not closed")
		}
	}
	t.Run("stream error preserved and sanitized for HTTP", func(t *testing.T) {
		expected := errors.New("SECRET-native-error")
		stream := &diagnosticFixtureStream[madmin.ServiceTraceInfo]{events: make(chan madmin.ServiceTraceInfo, 1), status: 200}
		stream.events <- madmin.ServiceTraceInfo{Err: expected}
		_, err := ReadDiagnostics(context.Background(), &diagnosticFixture{trace: stream}, DiagnosticOptions{Kind: "trace", Level: "ALL", Seconds: 1, Limit: 1})
		raw, _ := json.Marshal(apperror.Public(err))
		if !errors.Is(err, expected) || strings.Contains(string(raw), "SECRET") || stream.closes.Load() != 1 {
			t.Fatal(err, string(raw))
		}
	})
}

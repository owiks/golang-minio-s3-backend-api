package server

import (
	"context"
	"strings"
	"time"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/storage"
)

type DiagnosticOptions struct {
	Kind, Level    string
	Seconds, Limit int
}

func InvalidDiagnostic() error {
	return apperror.New(400, "InvalidDiagnostic", messages.ServerInvalidDiagnostic)
}

func ReadDiagnostics(parent context.Context, client ReadDiagnosticsClient, options DiagnosticOptions) (DiagnosticsReport, error) {
	if (options.Kind != "logs" && options.Kind != "trace") || options.Seconds < 1 || options.Seconds > 10 || options.Limit < 1 || options.Limit > 200 || (options.Level != "ALL" && options.Level != "ERROR") {
		return DiagnosticsReport{}, InvalidDiagnostic()
	}
	if err := parent.Err(); err != nil {
		return DiagnosticsReport{}, err
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(options.Seconds)*time.Second)
	defer cancel()
	started := time.Now()
	var report DiagnosticsReport
	var err error
	if options.Kind == "trace" {
		var stream storage.DiagnosticStream[madmin.ServiceTraceInfo]
		stream, err = client.OpenDiagnosticTrace(ctx, options.Level == "ERROR")
		if err == nil {
			defer stream.Close()
			report, err = collectDiagnostics(ctx, stream, options.Limit, func(v madmin.ServiceTraceInfo) (DiagnosticRow, bool, error) {
				return TraceRow(v.Trace), true, v.Err
			}, messages.ServerMinIOEndedTheTraceBeforeCollectionFinished)
		}
	} else {
		var stream storage.DiagnosticStream[madmin.LogInfo]
		stream, err = client.OpenDiagnosticLogs(ctx, options.Limit)
		if err == nil {
			defer stream.Close()
			report, err = collectDiagnostics(ctx, stream, options.Limit, func(v madmin.LogInfo) (DiagnosticRow, bool, error) {
				if v.Err != nil {
					return DiagnosticRow{}, false, v.Err
				}
				if options.Level == "ERROR" && !strings.EqualFold(v.Level, "ERROR") && !strings.EqualFold(v.Level, "FATAL") {
					return DiagnosticRow{}, false, nil
				}
				row := DiagnosticRow{Time: v.Time, Node: DiagnosticToken(v.NodeName), Level: DiagnosticToken(v.Level)}
				if v.API != nil {
					row.API = DiagnosticToken(v.API.Name)
				}
				return row, true, nil
			}, messages.ServerMinIOCouldNotOpenTheServerLog)
		}
	}
	// A collection interval ending normally is success; cancellation of the
	// parent operation must not be reported as a successful empty collection.
	if parent.Err() != nil {
		return DiagnosticsReport{}, parent.Err()
	}
	if err != nil {
		return DiagnosticsReport{}, err
	}
	report.Started, report.Ended = started, time.Now()
	return report, nil
}

func collectDiagnostics[T any](ctx context.Context, stream storage.DiagnosticStream[T], limit int, row func(T) (DiagnosticRow, bool, error), closedMessage messages.Ref) (DiagnosticsReport, error) {
	report := DiagnosticsReport{Rows: []DiagnosticRow{}}
	var err error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case event, ok := <-stream.Events():
			if ctx.Err() != nil {
				break loop
			}
			if !ok {
				err = apperror.New(502, "DiagnosticStreamClosed", closedMessage)
				break loop
			}
			value, keep, eventErr := row(event)
			if eventErr != nil {
				err = eventErr
				break loop
			}
			if keep {
				report.Rows = append(report.Rows, value)
			}
			if len(report.Rows) >= limit {
				report.Limited = true
				break loop
			}
		}
	}
	if failure := stream.Err(); failure != nil {
		return report, failure
	}
	switch stream.Status() {
	case 403:
		return report, apperror.New(403, "AccessDenied", messages.ApperrorInsufficientMinIOPermissionsForThisOperation)
	case 404, 501:
		return report, apperror.New(501, "NotImplemented", messages.ServerNotImplemented)
	}
	return report, err
}

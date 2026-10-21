package middleware

import (
	"context"
	"net/http"
	"time"

	"minio-web/api/src/services/telemetry"
)

type responseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
	ctx    context.Context
}

func (w *responseWriter) Failure(code string)         { telemetry.Failure(w.ctx, code) }
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += int64(n)
	if err != nil {
		telemetry.Failure(w.ctx, "ResponseWriteFailed")
	}
	return n, err
}
func (w *responseWriter) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	err := http.NewResponseController(w.ResponseWriter).Flush()
	if err != nil {
		telemetry.Failure(w.ctx, "ResponseWriteFailed")
	}
	return err
}

// Observe preserves ResponseController unwrapping, flushing and deadlines. A
// panic after headers must abort the stream, never append a JSON error to a file.
func Observe(w http.ResponseWriter, r *http.Request, operation string, metrics *telemetry.Metrics, next http.HandlerFunc) {
	ctx, trace := telemetry.Start(r.Context(), operation)
	r = r.WithContext(ctx)
	w.Header().Set("X-Request-ID", trace.ID)
	rw := &responseWriter{ResponseWriter: w, ctx: ctx}
	started := time.Now()
	metrics.Begin()
	defer func() {
		crashed := recover()
		abort := crashed != nil && (crashed == http.ErrAbortHandler || rw.status != 0)
		if crashed != nil {
			if crashed == http.ErrAbortHandler {
				if trace.ResultName(ctx, 200) == "success" {
					telemetry.Failure(ctx, "StreamAborted")
				}

			} else {
				telemetry.Failure(ctx, "Panic")
				if rw.status == 0 {
					rw.Header().Set("Content-Type", "application/json; charset=utf-8")
					rw.WriteHeader(500)
					_, _ = rw.Write([]byte("{\"error\":{\"code\":\"InternalError\",\"message\":\"Внутренняя ошибка API.\"}}\n"))
				}
			}
		}
		status := rw.status
		if status == 0 {
			status = 200
		}
		if crashed != nil && (crashed != http.ErrAbortHandler || rw.status == 0) {
			status = 500
		}
		elapsed := time.Since(started)
		metrics.End(operation, status, elapsed, trace.ResultName(ctx, status))
		if operation != "GET /health" && operation != "documentation" {
			trace.Log(ctx, status, rw.bytes, elapsed)
		}
		if abort {
			panic(http.ErrAbortHandler)
		}
	}()
	next(rw, r)
}

package integration

import (
	"context"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	uploadscontroller "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/uploads/controllers"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads/source"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

type rejectingStreamWriter struct {
	*httptest.ResponseRecorder
	phase           string
	writes, flushes int
}

func (w *rejectingStreamWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.phase == "write" {
		return 0, io.ErrClosedPipe
	}
	return w.ResponseRecorder.Write(p)
}
func (w *rejectingStreamWriter) FlushError() error {
	w.flushes++
	if w.phase == "flush" {
		return io.ErrClosedPipe
	}
	w.ResponseRecorder.Flush()
	return nil
}

func TestImportStreamFailureDoesNotCommitIntegration(t *testing.T) {
	_, s3, bucket := fileFixture(t)
	app := runtimesvc.New(testConfig())
	defer app.Close()
	for _, phase := range []string{"write", "flush", "source-error"} {
		t.Run(phase, func(t *testing.T) {
			key := phase + ".txt"
			client := remoteTestSource(func(ctx context.Context, raw string) (source.File, error) {
				if err := ctx.Err(); err != nil {
					return source.File{}, err
				}
				if phase == "source-error" {
					return source.File{}, apperror.New(502, "SourceResponse", messages.RemotesourceSourceResponse)
				}
				return source.File{Size: 4, Body: io.NopCloser(strings.NewReader("body")), ContentType: "text/plain"}, nil
			})
			ctx, trace := telemetry.Start(context.Background(), "POST import")
			r := httptest.NewRequest("POST", "/?"+url.Values{"bucket": {bucket}, "key": {key}}.Encode(), strings.NewReader(`{"url":"https://example.org/file","revision":"absent"}`)).WithContext(ctx)
			r.Header.Set("Content-Type", "application/json")
			w := &rejectingStreamWriter{ResponseRecorder: httptest.NewRecorder(), phase: phase}
			if err := uploadscontroller.HandleImportFromURL(app, w, r, testsession.Native(t), client); err != nil {
				t.Fatal("stream returned a second HTTP error", err)
			}
			if w.Header().Get("Content-Type") != "application/x-ndjson; charset=utf-8" || w.Header().Get("X-Accel-Buffering") != "no" || strings.Contains(w.Body.String(), `"result"`) || app.Temporary.Used() != 0 {
				t.Fatal("wrong stream headers, false success or leaked staging", w.Body.String())
			}
			if phase == "source-error" {
				if w.writes != 2 || w.flushes != 2 || strings.Count(w.Body.String(), `"error"`) != 1 || !strings.Contains(w.Body.String(), `"code":"SourceResponse"`) || trace.ResultName(ctx, 200) != "error" {
					t.Fatal("source failure not emitted once", w.Body.String(), trace.ResultName(ctx, 200))
				}
			} else {
				flushes := 0
				if phase == "flush" {
					flushes = 1
				}
				if w.writes != 1 || w.flushes != flushes || trace.ResultName(ctx, 200) != "cancelled" || trace.Code != "StreamWriteFailed" {
					t.Fatal("delivery retried or cancellation lost", w.writes, w.flushes, trace.Code)
				}
			}
			_, err := s3.StatObject(context.Background(), bucket, key, minio.StatObjectOptions{})
			if minio.ToErrorResponse(err).Code != "NoSuchKey" {
				t.Fatal("failed stream committed a destination", err)
			}
		})
	}
}

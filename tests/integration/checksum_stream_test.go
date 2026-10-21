package integration

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	objectscontroller "minio-web/api/src/http/api/routes/objects/controllers"
	runtimesvc "minio-web/api/src/services/runtime"
	"minio-web/api/src/services/telemetry"
	"minio-web/api/tests/testsession"
)

func TestChecksumStreamFailureDeliveryIntegration(t *testing.T) {
	_, s3, bucket := fileFixture(t)
	body := strings.Repeat("checksum", 32*1024)
	if _, err := s3.PutObject(context.Background(), bucket, "file.bin", strings.NewReader(body), int64(len(body)), minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	app := runtimesvc.New(testConfig())
	defer app.Close()
	for _, phase := range []string{"write", "flush"} {
		t.Run(phase, func(t *testing.T) {
			ctx, trace := telemetry.Start(context.Background(), "POST checksum")
			request := httptest.NewRequest("POST", "/?"+url.Values{"bucket": {bucket}, "key": {"file.bin"}}.Encode(), strings.NewReader(`{}`)).WithContext(ctx)
			request.Header.Set("Content-Type", "application/json")
			writer := &rejectingStreamWriter{ResponseRecorder: httptest.NewRecorder(), phase: phase}
			if err := objectscontroller.HandleCalculateChecksum(app, writer, request, testsession.Native(t)); err != nil {
				t.Fatal("delivery failure returned a second HTTP error", err)
			}
			flushes := 0
			if phase == "flush" {
				flushes = 1
			}
			if writer.writes != 1 || writer.flushes != flushes || strings.Contains(writer.Body.String(), `"result"`) || writer.Header().Get("Content-Type") != "application/x-ndjson; charset=utf-8" {
				t.Fatal("checksum retried a failed delivery or emitted success", writer.writes, writer.flushes, writer.Body.String())
			}
			if ctx.Err() != nil || trace.ResultName(ctx, 200) != "cancelled" || trace.Code != "StreamWriteFailed" {
				t.Fatal("delivery cancellation escaped its operation or lost its outcome", ctx.Err(), trace.Result, trace.Code)
			}
		})
	}
}

package server

import (
	"context"
	"testing"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

type metricsReader func(context.Context) ([]byte, error)

func (f metricsReader) ReadS3Metrics(ctx context.Context) ([]byte, error) { return f(ctx) }

func TestMetricsPortParsesPayloadAndSanitizesAdapterError(t *testing.T) {
	ctx := context.Background()
	reader := metricsReader(func(got context.Context) ([]byte, error) {
		if got != ctx {
			t.Fatal("context lost")
		}
		return []byte("# TYPE minio_s3_requests_total counter\nminio_s3_requests_total{api=\"getobject\",server=\"node:9000\"} 42\n"), nil
	})
	value, err := ReadLiveMetrics(ctx, reader)
	if err != nil || len(value.APIs) != 1 || value.APIs[0].Requests == nil || *value.APIs[0].Requests != 42 {
		t.Fatal("metrics parsing changed", value, err)
	}
	reader = func(context.Context) ([]byte, error) {
		return nil, apperror.New(502, "MetricsTooLarge", messages.Literal("bounded"))
	}
	if _, err = ReadLiveMetrics(ctx, reader); apperror.Public(err).Code != "ServerOperationFailed" {
		t.Fatal("public error sanitization changed", err)
	}
}

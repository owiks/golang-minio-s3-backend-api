package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/minio/madmin-go/v3"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
)

var errMetricsCaptured = errors.New("metrics response captured before SDK parsing")

type boundedMetricsTransport struct {
	Base http.RoundTripper `json:"-"`
	Data *[]byte           `json:"-"`
}

func (t boundedMetricsTransport) withTelemetry() http.RoundTripper {
	t.Base = observeTransport(t.Base)
	return t
}

func (t boundedMetricsTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, e := t.Base.RoundTrip(r)
	if e != nil {
		return nil, e
	}
	defer response.Body.Close()
	data, e := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
	if e != nil {
		return nil, e
	}
	if len(data) > 2*1024*1024 {
		return nil, apperror.New(502, "MetricsTooLarge", messages.StorageMetricsTooLarge)
	}
	if t.Data != nil {
		*t.Data = data
	}
	if response.StatusCode == http.StatusOK {
		// madmin-go still uses a zero-value Prometheus parser, which newer
		// expfmt releases reject. Keep SDK authentication, but parse the captured
		// response once in the service with an explicitly configured parser.
		return nil, errMetricsCaptured
	}
	response.Body = io.NopCloser(bytes.NewReader(data))
	return response, nil
}

func ReadS3Metrics(ctx context.Context, c Config, transport http.RoundTripper) ([]byte, error) {
	var raw []byte
	client, err := madmin.NewMetricsClientWithOptions(c.Endpoint.Host, &madmin.Options{Creds: c.Credentials, Secure: c.Endpoint.Scheme == "https", Transport: observeTransport(boundedMetricsTransport{Base: transport, Data: &raw})})
	if err != nil {
		return nil, err
	}
	if _, err = client.ClusterMetrics(ctx); err != nil {
		if errors.Is(err, errMetricsCaptured) {
			return raw, nil
		}
		telemetry.SDKError(ctx, err)
		return nil, err
	}
	return raw, nil
}

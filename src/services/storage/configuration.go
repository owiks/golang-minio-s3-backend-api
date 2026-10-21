package storage

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/cors"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
	"github.com/minio/minio-go/v7/pkg/sse"

	"minio-web/api/src/services/apperror"
)

// ConfigRead retains the exact upstream representation so business validation
// can reject SDK round trips that would lose unknown configuration fields.
type ConfigRead[T any] struct {
	Value T      `json:"-"`
	Raw   []byte `json:"-"`
}

// Capture the SDK's wire response so newer fields cannot silently disappear in
// a read/modify/write through an older SDK. Credentials and endpoint transport
// remain those of this session. No XML is exposed to the browser.
type configurationTransport struct {
	Base   http.RoundTripper `json:"-"`
	Raw    []byte            `json:"-"`
	Status int               `json:"-"`
	Query  string            `json:"-"`
}

func (wire *configurationTransport) withTelemetry() http.RoundTripper {
	wire.Base = observeTransport(wire.Base)
	return wire
}

func (wire *configurationTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := wire.Base.RoundTrip(r)
	if err != nil {
		return resp, err
	}
	query := wire.Query
	if query == "" {
		query = "lifecycle"
	}
	if r.URL.Query().Has(query) {
		wire.Status = resp.StatusCode
		if r.Method == "GET" && resp.StatusCode == 200 {
			raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
			resp.Body.Close()
			if readErr != nil {
				return nil, readErr
			}
			if len(raw) > 4*1024*1024 {
				return nil, apperror.New(502, "LifecycleTooLarge", "Конфигурация Lifecycle превышает 4 МиБ. Изменения не выполнялись.")
			}
			wire.Raw = raw
			resp.Body = io.NopCloser(bytes.NewReader(raw))
		}
	}
	return resp, nil
}

func configurationClient(c Config, tr http.RoundTripper, query string) (*minio.Client, *configurationTransport, error) {
	wire := &configurationTransport{Base: tr, Query: query}
	client, err := NewS3(c, wire)
	return client, wire, err
}
func ReadLifecycleConfig(ctx context.Context, c Config, tr http.RoundTripper, bucket string) (ConfigRead[*lifecycle.Configuration], error) {
	client, wire, err := configurationClient(c, tr, "lifecycle")
	if err != nil {
		return ConfigRead[*lifecycle.Configuration]{}, err
	}
	value, err := client.GetBucketLifecycle(ctx, bucket)
	return ConfigRead[*lifecycle.Configuration]{Value: value, Raw: wire.Raw}, err
}
func WriteLifecycleConfig(ctx context.Context, c Config, tr http.RoundTripper, bucket string, value *lifecycle.Configuration) (int, error) {
	client, wire, err := configurationClient(c, tr, "lifecycle")
	if err != nil {
		return 0, err
	}
	err = client.SetBucketLifecycle(ctx, bucket, value)
	return wire.Status, err
}
func ReadCORSConfig(ctx context.Context, c Config, tr http.RoundTripper, bucket string) (ConfigRead[*cors.Config], error) {
	client, wire, err := configurationClient(c, tr, "cors")
	if err != nil {
		return ConfigRead[*cors.Config]{}, err
	}
	value, err := client.GetBucketCors(ctx, bucket)
	return ConfigRead[*cors.Config]{Value: value, Raw: wire.Raw}, err
}
func WriteCORSConfig(ctx context.Context, c Config, tr http.RoundTripper, bucket string, value *cors.Config) (int, error) {
	client, wire, err := configurationClient(c, tr, "cors")
	if err != nil {
		return 0, err
	}
	err = client.SetBucketCors(ctx, bucket, value)
	return wire.Status, err
}
func ReadEncryptionConfig(ctx context.Context, c Config, tr http.RoundTripper, bucket string) (ConfigRead[*sse.Configuration], error) {
	client, wire, err := configurationClient(c, tr, "encryption")
	if err != nil {
		return ConfigRead[*sse.Configuration]{}, err
	}
	value, err := client.GetBucketEncryption(ctx, bucket)
	return ConfigRead[*sse.Configuration]{Value: value, Raw: wire.Raw}, err
}
func WriteEncryptionConfig(ctx context.Context, c Config, tr http.RoundTripper, bucket string, value *sse.Configuration) (int, error) {
	client, wire, err := configurationClient(c, tr, "encryption")
	if err != nil {
		return 0, err
	}
	if value == nil {
		err = client.RemoveBucketEncryption(ctx, bucket)
	} else {
		err = client.SetBucketEncryption(ctx, bucket, value)
	}
	return wire.Status, err
}

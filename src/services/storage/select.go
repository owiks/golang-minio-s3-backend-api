package storage

import (
	"context"
	"io"
	"net/http"
	"sync"

	"github.com/minio/minio-go/v7"
)

// SelectStream owns the SDK reader and its request. FinalStats is meaningful
// only after Read returns EOF; it never reads the SDK's concurrently-mutated
// progress field while the decoder is running.
type SelectStream interface {
	io.ReadCloser
	FinalStats() *minio.StatsMessage
}

type sdkSelectStream struct {
	reader    *minio.SelectResults
	body      *selectBody
	cancel    context.CancelFunc
	closeOnce sync.Once
	closeErr  error
	complete  bool
}

func OpenSelect(parent context.Context, c Config, transport http.RoundTripper, bucket, key string, options minio.SelectObjectOptions) (SelectStream, error) {
	ctx, cancel := context.WithCancel(parent)
	scope := &selectScope{}
	ctx = context.WithValue(ctx, selectScopeKey{}, scope)
	client, err := NewS3(c, transport)
	if err != nil {
		cancel()
		return nil, err
	}
	reader, err := client.SelectObjectContent(ctx, bucket, key, options)
	if err != nil {
		cancel()
		return nil, err
	}
	body := scope.body.Load()
	if body == nil {
		cancel()
		_ = reader.Close()
		return nil, invalidSelectEvent()
	}
	return &sdkSelectStream{reader: reader, body: body, cancel: cancel}, nil
}

func (s *sdkSelectStream) Read(p []byte) (int, error) {
	n, err := s.reader.Read(p)
	if err == io.EOF {
		if !s.body.ended.Load() {
			return n, invalidSelectEvent()
		}
		s.complete = true
	}
	return n, err
}
func (s *sdkSelectStream) FinalStats() *minio.StatsMessage {
	if !s.complete || !s.body.stats.Load() {
		return nil
	}
	value := *s.reader.Stats()
	return &value
}
func (s *sdkSelectStream) Close() error {
	s.closeOnce.Do(func() { s.cancel(); _ = s.body.Close(); s.closeErr = s.reader.Close() })
	return s.closeErr
}

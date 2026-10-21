package telemetry

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestUpstreamMeasurementsKeepCancellationAndAreConcurrentSafe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ctx, trace := Start(ctx, "GET test")
	cancel()
	base := transportFunc(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })
	transport := Transport{Base: base}
	r, _ := http.NewRequestWithContext(ctx, "GET", "http://PRIVATE-UPSTREAM/?secret=PRIVATE-TOKEN", nil)
	var jobs sync.WaitGroup
	for range 20 {
		jobs.Go(func() {
			response, err := transport.RoundTrip(r)
			if response != nil {
				_ = response.Body.Close()
			}
			if !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		})
	}
	jobs.Wait()
	if trace.UpstreamCalls != 20 {
		t.Fatal(trace.UpstreamCalls)
	}
}

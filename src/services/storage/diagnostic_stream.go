package storage

import (
	"context"
	"sync"
)

// DiagnosticStream owns an SDK subscription. Close cancels the upstream request
// and drains terminal sends: some SDK error paths do not select on ctx.Done().
// Status is the last upstream HTTP status, including errors omitted by the SDK.
type DiagnosticStream[T any] interface {
	Events() <-chan T
	Status() int
	Err() error
	Close()
}

type diagnosticStream[T any] struct {
	events  <-chan T
	cancel  context.CancelFunc
	status  func() int
	failure func() error
	once    sync.Once
}

func newDiagnosticStream[T any](events <-chan T, cancel context.CancelFunc, status func() int, failure func() error) DiagnosticStream[T] {
	return &diagnosticStream[T]{events: events, cancel: cancel, status: status, failure: failure}
}

func (s *diagnosticStream[T]) Events() <-chan T { return s.events }
func (s *diagnosticStream[T]) Status() int      { return s.status() }
func (s *diagnosticStream[T]) Err() error {
	if s.failure == nil {
		return nil
	}
	return s.failure()
}
func (s *diagnosticStream[T]) Close() {
	s.once.Do(func() {
		s.cancel()
		go func() {
			for range s.events {
			}
		}()
	})
}

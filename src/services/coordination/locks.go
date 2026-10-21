// Package coordination owns bounded, cancellable coordination between requests.
package coordination

import (
	"context"
	"sync"
)

type lock struct {
	token chan struct{}
	refs  int
}
type Locks struct {
	mu      sync.Mutex
	entries map[string]*lock
}

// Acquire serializes one resource across sessions. Waiting consumes the request's
// deadline; unused entries are removed, including cancelled waiters.
func (l *Locks) Acquire(ctx context.Context, key string) (func(), error) {
	l.mu.Lock()
	if l.entries == nil {
		l.entries = make(map[string]*lock)
	}
	entry := l.entries[key]
	if entry == nil {
		entry = &lock{token: make(chan struct{}, 1)}
		l.entries[key] = entry
	}
	entry.refs++
	l.mu.Unlock()
	drop := func() {
		l.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(l.entries, key)
		}
		l.mu.Unlock()
	}
	select {
	case entry.token <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-entry.token
			drop()
			return nil, err
		}
		var once sync.Once
		return func() { once.Do(func() { <-entry.token; drop() }) }, nil
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	}
}

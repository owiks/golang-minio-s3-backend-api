package commandlog

import (
	"context"
	"sync"
	"time"
)

type memoryKey struct {
	scope Scope
	hash  string
}

type Memory struct {
	mu      sync.Mutex
	entries map[memoryKey]Entry
	closed  bool
}

func NewMemory() *Memory { return &Memory{entries: map[memoryKey]Entry{}} }

func (m *Memory) Reserve(ctx context.Context, scope Scope, keyHash, fingerprint string, now time.Time) (Entry, bool, error) {
	if !ValidReservation(scope, keyHash, fingerprint, now) {
		return Entry{}, false, ErrInvalid
	}
	if ctx.Err() != nil {
		return Entry{}, false, ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Entry{}, false, ErrUnavailable
	}
	for key, entry := range m.entries {
		if !now.Before(entry.ExpiresAt) {
			delete(m.entries, key)
		}
	}
	key := memoryKey{scope: scope, hash: keyHash}
	if entry, found := m.entries[key]; found {
		next, err := Replay(entry, fingerprint, now)
		m.entries[key] = next.Clone()
		return next.Clone(), false, err
	}
	count := 0
	for key := range m.entries {
		if key.scope.ActorID == scope.ActorID {
			count++
		}
	}
	if count >= MaxPerActor || len(m.entries) >= MaxRecords {
		return Entry{}, false, ErrLimit
	}
	entry := NewEntry(scope, keyHash, fingerprint, now)
	m.entries[key] = entry
	return entry.Clone(), true, nil
}

func (m *Memory) Finish(ctx context.Context, id string, scope Scope, response *Response) error {
	if id == "" || !scope.Valid() || response != nil && !response.Valid() {
		return ErrInvalid
	}
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrUnavailable
	}
	for key, entry := range m.entries {
		if entry.ID != id || entry.Scope != scope {
			continue
		}
		if entry.State != Pending {
			return ErrConflict
		}
		entry.State, entry.Response = Unknown, nil
		if response != nil {
			entry.State, entry.Response = Completed, response
		}
		m.entries[key] = entry.Clone()
		return nil
	}
	return ErrNotFound
}

func (m *Memory) Ping(ctx context.Context) error {
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrUnavailable
	}
	return nil
}

func (m *Memory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	clear(m.entries)
	return nil
}

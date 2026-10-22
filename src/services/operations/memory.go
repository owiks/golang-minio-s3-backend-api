package operations

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"
)

type Memory struct {
	mu      sync.Mutex
	records map[string]Snapshot
	closed  bool
}

func NewMemory() *Memory { return &Memory{records: map[string]Snapshot{}} }
func (m *Memory) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.closed {
		return ErrUnavailable
	}
	return nil
}
func (m *Memory) Insert(ctx context.Context, s Scope, p Spec, d, n time.Time) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return Snapshot{}, err
	}
	return InsertRecord(m.records, s, p, d, n)
}
func (m *Memory) Get(ctx context.Context, s Scope, id string) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return Snapshot{}, err
	}
	v, ok := m.records[id]
	if !ok || v.Scope != s {
		return Snapshot{}, ErrNotFound
	}
	return v.Clone(), nil
}
func (m *Memory) List(ctx context.Context, s Scope) ([]Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return nil, err
	}
	v := []Snapshot{}
	for _, r := range m.records {
		if r.Scope == s {
			v = append(v, r.Clone())
		}
	}
	sort.Slice(v, func(i, j int) bool { return v[i].CreatedAt.After(v[j].CreatedAt) })
	return v, nil
}
func (m *Memory) Claim(ctx context.Context, w string, n time.Time, l time.Duration) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return Snapshot{}, err
	}
	return ClaimRecord(m.records, w, n, l)
}
func (m *Memory) Save(ctx context.Context, id, lease string, revision uint64, c Checkpoint, n time.Time) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return Snapshot{}, err
	}
	s, ok := m.records[id]
	if !ok {
		return Snapshot{}, ErrNotFound
	}
	v, err := SaveRecord(s, lease, revision, c, n)
	if err == nil {
		m.records[id] = v.Clone()
	}
	return v, err
}
func (m *Memory) Renew(ctx context.Context, id, lease string, n time.Time, l time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return err
	}
	s, ok := m.records[id]
	if !ok || s.State != Running || s.LeaseID != lease || !n.Before(s.LeaseUntil) {
		return ErrConflict
	}
	if l < time.Second || l > time.Hour {
		return ErrInvalid
	}
	n = recordTime(s, n)
	if until := normalized(n.Add(l)); until.After(s.LeaseUntil) {
		s.LeaseUntil = until
	}
	m.records[id] = s
	return nil
}
func (m *Memory) Cancel(ctx context.Context, scope Scope, id string, rev uint64, n time.Time) (Snapshot, error) {
	return m.mutate(ctx, scope, id, rev, n, CancelRecord)
}
func (m *Memory) Retry(ctx context.Context, scope Scope, id string, rev uint64, n time.Time) (Snapshot, error) {
	return m.mutate(ctx, scope, id, rev, n, RetryRecord)
}
func (m *Memory) mutate(ctx context.Context, scope Scope, id string, rev uint64, n time.Time, fn func(Snapshot, uint64, time.Time) (Snapshot, error)) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return Snapshot{}, err
	}
	s, ok := m.records[id]
	if !ok || s.Scope != scope {
		return Snapshot{}, ErrNotFound
	}
	v, err := fn(s, rev, n)
	if err == nil {
		m.records[id] = v.Clone()
	}
	return v, err
}
func (m *Memory) Prune(ctx context.Context, n time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return err
	}
	PruneRecords(m.records, n)
	return nil
}
func (m *Memory) Ping(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.check(ctx)
}
func (m *Memory) Close() error { m.mu.Lock(); defer m.mu.Unlock(); m.closed = true; return nil }

func (m *Memory) Export(ctx context.Context) ([]Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return nil, err
	}
	out := make([]Snapshot, 0, len(m.records))
	size := 0
	for _, s := range m.records {
		raw, err := json.Marshal(s)
		if err != nil {
			return nil, ErrInvalid
		}
		if len(raw) > MaxSnapshotBytes-size {
			return nil, ErrLimit
		}
		size += len(raw)
		out = append(out, s.Clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (m *Memory) Import(ctx context.Context, records []Snapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return err
	}
	if !ValidBackup(records) {
		return ErrInvalid
	}
	incoming := map[string]Snapshot{}
	for _, s := range records {
		incoming[s.ID] = s
	}
	for id, old := range m.records {
		next, ok := incoming[id]
		if !ok || !Same(old, next) {
			return ErrConflict
		}
	}
	for _, s := range records {
		m.records[s.ID] = s.Clone()
	}
	return nil
}

package authstate

import (
	"context"
	"time"
)

// Prunable returns only accesses whose absolute expiry precedes the retention
// boundary, and old connections which no remaining access references.
func Prunable(r Records, before time.Time) ([]string, []string) {
	accesses, connections := []string{}, []string{}
	remaining := map[string]bool{}
	retained := map[string]bool{}
	for id, a := range r.Accesses {
		d := a.Snapshot()
		if !d.ExpiresAt.Before(before) {
			// Keep the ancestry of retained grants even if an ancestor has been
			// revoked and its expiry is older than the retention boundary.
			for current := id; current != "" && !retained[current]; {
				ancestor, ok := r.Accesses[current]
				if !ok {
					break
				}
				retained[current] = true
				current = ancestor.Snapshot().ParentID
			}
		}
	}
	for id, a := range r.Accesses {
		if retained[id] {
			remaining[a.Snapshot().ConnectionID] = true
		} else {
			accesses = append(accesses, id)
		}
	}
	for id, c := range r.Connections {
		if !remaining[id] && c.Snapshot().CreatedAt.Before(before) {
			connections = append(connections, id)
		}
	}
	return accesses, connections
}
func (m *Manager) Prune(ctx context.Context, before time.Time) (int, error) {
	if before.IsZero() || !before.Before(m.now()) {
		return 0, Invalid()
	}
	return m.repository.Prune(ctx, before)
}
func (m *Memory) Prune(ctx context.Context, before time.Time) (int, error) {
	if before.IsZero() || !before.Before(m.now()) {
		return 0, Invalid()
	}
	if ctx.Err() != nil {
		return 0, Unavailable()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, Unavailable()
	}
	a, c := Prunable(Records{m.connections, m.accesses}, before)
	for _, id := range a {
		delete(m.accesses, id)
	}
	for _, id := range c {
		delete(m.connections, id)
	}
	return len(a) + len(c), nil
}

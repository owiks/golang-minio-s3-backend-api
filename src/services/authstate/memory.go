package authstate

import (
	"context"
	"sort"
	"sync"
)

type Memory struct {
	mu          sync.RWMutex
	connections map[string]Connection
	accesses    map[string]Access
	events      []Event
	closed      bool
}

func NewMemory() *Memory {
	return &Memory{connections: map[string]Connection{}, accesses: map[string]Access{}}
}
func (m *Memory) Ping(ctx context.Context) error {
	if ctx.Err() != nil {
		return Unavailable()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return Unavailable()
	}
	return nil
}
func (m *Memory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	clear(m.connections)
	clear(m.accesses)
	m.events = nil
	return nil
}
func (m *Memory) Bootstrap(ctx context.Context, c Connection, a Access, e Event) error {
	if ctx.Err() != nil {
		return Unavailable()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Unavailable()
	}
	if m.hasEvent(e.ID) {
		return Unavailable()
	}
	if len(m.connections) >= 16384 || len(m.accesses) >= 16384 {
		return Limit()
	}
	if err := ValidateBootstrap(Records{m.connections, m.accesses}, c, a); err != nil {
		return err
	}
	m.connections[c.Snapshot().ID] = c
	m.accesses[a.Snapshot().ID] = a
	m.events = append(m.events, e)
	if len(m.events) > 10000 {
		m.events = append([]Event(nil), m.events[len(m.events)-10000:]...)
	}
	return nil
}
func (m *Memory) Get(ctx context.Context, id string) (Access, Connection, error) {
	if ctx.Err() != nil {
		return Access{}, Connection{}, Unavailable()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return Access{}, Connection{}, Unavailable()
	}
	a, ok := m.accesses[id]
	if !ok {
		return Access{}, Connection{}, NotFound()
	}
	c, ok := m.connections[a.Snapshot().ConnectionID]
	if !ok {
		return Access{}, Connection{}, Unavailable()
	}
	lookup := func(parentID string) (Access, Connection, error) {
		parent, found := m.accesses[parentID]
		if !found {
			return Access{}, Connection{}, NotFound()
		}
		parentConnection, found := m.connections[parent.Snapshot().ConnectionID]
		if !found {
			return Access{}, Connection{}, Unavailable()
		}
		return parent, parentConnection, nil
	}
	if err := ValidateLineage(a, c, lookup); err != nil {
		return Access{}, Connection{}, err
	}
	return a, c, nil
}
func (m *Memory) Connections(ctx context.Context, owner string) ([]Connection, error) {
	if ctx.Err() != nil {
		return nil, Unavailable()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return nil, Unavailable()
	}
	out := []Connection{}
	for _, c := range m.connections {
		if c.Snapshot().Owner == owner {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Snapshot().ID < out[j].Snapshot().ID })
	return out, nil
}
func (m *Memory) Accesses(ctx context.Context, owner, cid string) ([]Access, error) {
	if ctx.Err() != nil {
		return nil, Unavailable()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return nil, Unavailable()
	}
	c, ok := m.connections[cid]
	if !ok || c.Snapshot().Owner != owner {
		return nil, NotFound()
	}
	out := []Access{}
	for _, a := range m.accesses {
		if a.Snapshot().Owner == owner && a.Snapshot().ConnectionID == cid {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Snapshot().ID < out[j].Snapshot().ID })
	return out, nil
}
func (m *Memory) Events(ctx context.Context, owner string) ([]Event, error) {
	if ctx.Err() != nil {
		return nil, Unavailable()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return nil, Unavailable()
	}
	out := []Event{}
	for i := len(m.events) - 1; i >= 0 && len(out) < 10000; i-- {
		if m.events[i].Owner == owner {
			out = append(out, m.events[i])
		}
	}
	return out, nil
}
func (m *Memory) Apply(ctx context.Context, cmd Command) (Result, error) {
	if ctx.Err() != nil {
		return Result{}, Unavailable()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Result{}, Unavailable()
	}
	if m.hasEvent(cmd.Event.ID) {
		return Result{}, Unavailable()
	}
	if cmd.Kind == "create" && len(m.accesses) >= 16384 {
		return Result{}, Limit()
	}
	changes, err := Transition(Records{m.connections, m.accesses}, cmd)
	if err != nil {
		return Result{}, err
	}
	for _, c := range changes.Connections {
		m.connections[c.Snapshot().ID] = c
	}
	for _, a := range changes.Accesses {
		m.accesses[a.Snapshot().ID] = a
	}
	m.events = append(m.events, changes.Event)
	if len(m.events) > 10000 {
		m.events = append([]Event(nil), m.events[len(m.events)-10000:]...)
	}
	return changes.Result, nil
}
func (m *Memory) Export(ctx context.Context) (Backup, error) {
	if ctx.Err() != nil {
		return Backup{}, Unavailable()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return Backup{}, Unavailable()
	}
	b := Backup{Version: 1, Connections: []ConnectionData{}, Accesses: []AccessData{}, Events: append([]Event{}, m.events...)}
	for _, c := range m.connections {
		b.Connections = append(b.Connections, c.Snapshot())
	}
	for _, a := range m.accesses {
		b.Accesses = append(b.Accesses, a.Snapshot())
	}
	return b, nil
}
func (m *Memory) Import(ctx context.Context, b Backup) error {
	if err := ValidateBackup(b); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return Unavailable()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Unavailable()
	}
	if len(m.connections) > 0 || len(m.accesses) > 0 || b.Version != 1 {
		return Conflict()
	}
	for _, c := range b.Connections {
		m.connections[c.ID] = NewConnection(c)
	}
	for _, a := range b.Accesses {
		m.accesses[a.ID] = NewAccess(a)
	}
	m.events = append([]Event{}, b.Events...)
	return nil
}

func (m *Memory) hasEvent(id string) bool {
	for _, e := range m.events {
		if e.ID == id {
			return true
		}
	}
	return false
}

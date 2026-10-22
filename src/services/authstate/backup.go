package authstate

import (
	"context"
	"time"
)

func ValidateBackup(b Backup) error {
	if b.Version != 1 || len(b.Connections) > 16384 || len(b.Accesses) > 16384 || len(b.Events) > 10000 {
		return Invalid()
	}
	connections := map[string]ConnectionData{}
	ids := map[string]bool{}
	for _, c := range b.Connections {
		if !ValidConnection(c) || ids[c.ID] || (c.Active && (c.Cipher.KeyID == "" || len(c.Cipher.Data) < 28)) {
			return Invalid()
		}
		ids[c.ID] = true
		connections[c.ID] = c
	}
	ids = map[string]bool{}
	records := Records{Connections: make(map[string]Connection, len(connections)), Accesses: make(map[string]Access, len(b.Accesses))}
	for id, c := range connections {
		records.Connections[id] = NewConnection(c)
	}
	for _, a := range b.Accesses {
		c, ok := connections[a.ConnectionID]
		if !ok || !ValidAccess(a) || ids[a.ID] || a.Owner != c.Owner || a.Revision < 1 || a.Generation < 1 || len(a.Digest) != 64 || !a.Policy.Valid() || (a.Mode != "cookie" && a.Mode != "bearer") {
			return Invalid()
		}
		ids[a.ID] = true
		records.Accesses[a.ID] = NewAccess(a)
	}
	if ValidateLineages(records) != nil {
		return Invalid()
	}
	ids = map[string]bool{}
	for _, e := range b.Events {
		if e.ID == "" || e.Owner == "" || ids[e.ID] {
			return Invalid()
		}
		ids[e.ID] = true
	}
	return nil
}
func (m *Memory) Touch(ctx context.Context, id string, now time.Time) error {
	if ctx.Err() != nil {
		return Unavailable()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Unavailable()
	}
	a, ok := m.accesses[id]
	if !ok {
		return NotFound()
	}
	d := a.Snapshot()
	if a.Valid(now) && now.Sub(d.LastUsedAt) >= time.Minute {
		d.LastUsedAt = now
		m.accesses[id] = NewAccess(d)
	}
	return nil
}
func (m *Memory) Reencrypt(ctx context.Context, c Connection) error {
	if ctx.Err() != nil {
		return Unavailable()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Unavailable()
	}
	d := c.Snapshot()
	old, ok := m.connections[d.ID]
	if !ok {
		return NotFound()
	}
	o := old.Snapshot()
	if o.Revision != d.Revision || o.SecretVersion != d.SecretVersion {
		return Conflict()
	}
	o.Cipher = d.Cipher
	o.Revision++
	m.connections[d.ID] = NewConnection(o)
	m.events = append(m.events, ReencryptionEvent(c))
	if len(m.events) > 10000 {
		m.events = append([]Event(nil), m.events[len(m.events)-10000:]...)
	}
	return nil
}

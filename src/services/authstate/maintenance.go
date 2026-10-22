package authstate

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

// Verify prevents serving a database whose active secrets cannot be decrypted.
func (m *Manager) Verify(ctx context.Context) error {
	b, err := m.repository.Export(ctx)
	if err != nil {
		return err
	}
	if err = ValidateBackup(b); err != nil {
		return Unavailable()
	}
	for _, d := range b.Connections {
		if ctx.Err() != nil {
			return Unavailable()
		}
		if d.Active {
			if _, err = m.Decrypt(NewConnection(d)); err != nil {
				return err
			}
		}
	}
	return nil
}
func (m *Manager) Export(ctx context.Context) (Backup, error) {
	b, err := m.repository.Export(ctx)
	if err != nil {
		return Backup{}, err
	}
	sort.Slice(b.Connections, func(i, j int) bool { return b.Connections[i].ID < b.Connections[j].ID })
	sort.Slice(b.Accesses, func(i, j int) bool { return b.Accesses[i].ID < b.Accesses[j].ID })
	return b, nil
}

// Restore requires an empty destination. By default an old backup cannot revive
// revoked credentials; preserving them is reserved for an offline DB transfer.
func (m *Manager) Restore(ctx context.Context, b Backup, preserve bool) error {
	if err := ValidateBackup(b); err != nil {
		return err
	}
	for _, d := range b.Connections {
		if ctx.Err() != nil {
			return Unavailable()
		}
		if d.Active {
			if _, err := m.Decrypt(NewConnection(d)); err != nil {
				return err
			}
		}
	}
	b.Accesses = append([]AccessData(nil), b.Accesses...)
	if !preserve {
		for i := range b.Accesses {
			b.Accesses[i].Active = false
			b.Accesses[i].PreviousDigest = ""
			b.Accesses[i].PreviousUntil = b.Accesses[i].CreatedAt
			b.Accesses[i].Revision++
		}
	}
	return m.repository.Import(ctx, b)
}

// Reencrypt is restartable: successfully replaced rows use ActiveID and are
// skipped on retry. CAS prevents overwriting a concurrent credential change.
func (m *Manager) Reencrypt(ctx context.Context) (int, error) {
	b, err := m.repository.Export(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, d := range b.Connections {
		if ctx.Err() != nil {
			return n, Unavailable()
		}
		if !d.Active || d.Cipher.KeyID == m.box.ActiveID() {
			continue
		}
		secrets, err := m.Decrypt(NewConnection(d))
		if err != nil {
			return n, err
		}
		raw, err := json.Marshal(secrets)
		if err != nil {
			return n, Unavailable()
		}
		d.Cipher, err = m.box.Seal(raw, NewConnection(d).AAD())
		clear(raw)
		if err != nil {
			return n, Unavailable()
		}
		if err = m.repository.Reencrypt(ctx, NewConnection(d)); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func ReencryptionEvent(c Connection) Event {
	return ReencryptionEventAt(c, time.Now())
}

// ReencryptionEventAt records maintenance using the repository clock.
func ReencryptionEventAt(c Connection, now time.Time) Event {
	d := c.Snapshot()
	return eventAt(d.Owner, "maintenance", d.ID, "connection.reencrypted", now)
}

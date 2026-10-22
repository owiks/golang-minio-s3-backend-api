package authstate

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"
)

func lineageFixture() Records {
	now := time.Now().UTC()
	c := ConnectionData{ID: "connection", Owner: Digest("owner"), Endpoint: "https://s3.example.test", Actor: Digest("actor"), Revision: 1, SecretVersion: 1, Active: true, CreatedAt: now, Cipher: Ciphertext{KeyID: "key", Data: make([]byte, 28)}}
	root := AccessData{ID: "root", ConnectionID: c.ID, Owner: c.Owner, Mode: "bearer", Renewable: true, Revision: 1, Generation: 1, Active: true, CreatedAt: now, ExpiresAt: now.Add(time.Hour), Digest: Digest("root-secret"), Policy: FullPolicy()}
	child := root
	child.ID, child.ParentID, child.Digest = "child", root.ID, Digest("child-secret")
	child.Policy = Policy{Scopes: []string{"objects:read"}, Resources: []Resource{{Bucket: "data", Prefix: "reports/"}}}
	return Records{Connections: map[string]Connection{c.ID: NewConnection(c)}, Accesses: map[string]Access{root.ID: NewAccess(root), child.ID: NewAccess(child)}}
}

func TestValidateLineageRejectsUnsafeDelegations(t *testing.T) {
	tests := map[string]func(Records){
		"missing parent": func(r Records) { delete(r.Accesses, "root") },
		"cycle": func(r Records) {
			d := r.Accesses["root"].Snapshot()
			d.ParentID = "child"
			r.Accesses[d.ID] = NewAccess(d)
		},
		"different owner": func(r Records) {
			d := r.Accesses["root"].Snapshot()
			d.Owner = Digest("other")
			r.Accesses[d.ID] = NewAccess(d)
		},
		"cookie parent": func(r Records) {
			d := r.Accesses["root"].Snapshot()
			d.Mode, d.Renewable, d.CSRFDigest = "cookie", false, Digest("csrf")
			r.Accesses[d.ID] = NewAccess(d)
		},
		"revoked parent": func(r Records) {
			d := r.Accesses["root"].Snapshot()
			d.Active = false
			r.Accesses[d.ID] = NewAccess(d)
		},
		"wider policy": func(r Records) {
			d := r.Accesses["root"].Snapshot()
			d.Policy = Policy{Scopes: []string{"objects:write"}}
			r.Accesses[d.ID] = NewAccess(d)
		},
		"later expiry": func(r Records) {
			d := r.Accesses["child"].Snapshot()
			d.ExpiresAt = d.ExpiresAt.Add(time.Second)
			r.Accesses[d.ID] = NewAccess(d)
		},
		"revoked ancestor connection": func(r Records) {
			d := r.Connections["connection"].Snapshot()
			d.Active = false
			r.Connections[d.ID] = NewConnection(d)
		},
	}
	for name, alter := range tests {
		t.Run(name, func(t *testing.T) {
			r := lineageFixture()
			alter(r)
			if ValidateLineages(r) == nil {
				t.Fatal("unsafe ancestry accepted")
			}
		})
	}
}

func TestValidateLineageDepthAndInactiveRecords(t *testing.T) {
	r := lineageFixture()
	previous := "root"
	for depth := 1; depth <= MaxDelegationDepth+1; depth++ {
		d := r.Accesses["root"].Snapshot()
		d.ID, d.ParentID = fmt.Sprintf("level-%d", depth), previous
		r.Accesses[d.ID] = NewAccess(d)
		lookup := func(id string) (Access, Connection, error) {
			return r.Accesses[id], r.Connections["connection"], nil
		}
		err := ValidateLineage(r.Accesses[d.ID], r.Connections["connection"], lookup)
		if depth <= MaxDelegationDepth && err != nil || depth > MaxDelegationDepth && err == nil {
			t.Fatalf("depth %d: %v", depth, err)
		}
		previous = d.ID
	}
	r = lineageFixture()
	root, child := r.Accesses["root"].Snapshot(), r.Accesses["child"].Snapshot()
	root.Active, child.Active = false, false
	root.Policy = Policy{Scopes: []string{"objects:write"}}
	root.ExpiresAt = child.ExpiresAt.Add(-time.Minute)
	r.Accesses[root.ID], r.Accesses[child.ID] = NewAccess(root), NewAccess(child)
	if err := ValidateLineages(r); err != nil {
		t.Fatal("revoked child no longer readable", err)
	}
}

func TestLegacyAccessAndBackupLineage(t *testing.T) {
	r := lineageFixture()
	root := r.Accesses["root"].Snapshot()
	root.Renewable = false
	r.Accesses["root"] = NewAccess(root)
	b := Backup{Version: 1, Connections: []ConnectionData{r.Connections["connection"].Snapshot()}, Accesses: []AccessData{root}}
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var legacy Backup
	if err = json.Unmarshal(raw, &legacy); err != nil || ValidateBackup(legacy) != nil || legacy.Accesses[0].Renewable || legacy.Accesses[0].ParentID != "" {
		t.Fatal("legacy backup changed", err)
	}
	b.Accesses = append(b.Accesses, r.Accesses["child"].Snapshot())
	if err = ValidateBackup(b); err != nil {
		t.Fatal("valid legacy parent rejected", err)
	}
	b.Accesses[1].ParentID = "missing"
	if ValidateBackup(b) == nil {
		t.Fatal("orphan restored")
	}
	b.Accesses[1].ParentID = b.Accesses[1].ID
	if ValidateBackup(b) == nil {
		t.Fatal("self-parent restored")
	}
}

func TestMemoryGetValidatesStoredLineage(t *testing.T) {
	r := lineageFixture()
	m := NewMemory()
	m.connections, m.accesses = r.Connections, r.Accesses
	if _, _, err := m.Get(context.Background(), "child"); err != nil {
		t.Fatal(err)
	}
	d := m.accesses["root"].Snapshot()
	d.Active = false
	m.accesses[d.ID] = NewAccess(d)
	if _, _, err := m.Get(context.Background(), "child"); err == nil {
		t.Fatal("revoked ancestor accepted")
	}
	child := m.accesses["child"].Snapshot()
	child.Active = false
	m.accesses[child.ID] = NewAccess(child)
	if a, _, err := m.Get(context.Background(), "child"); err != nil || a.Snapshot().Active {
		t.Fatal("revoked grant cannot be inspected", err)
	}
}

func TestPrunableRetainsReferencedAncestors(t *testing.T) {
	r := lineageFixture()
	now := time.Now().UTC()
	boundary := now.Add(-24 * time.Hour)
	root := r.Accesses["root"].Snapshot()
	root.Active = false
	root.ExpiresAt = now.Add(-72 * time.Hour)
	r.Accesses[root.ID] = NewAccess(root)
	child := r.Accesses["child"].Snapshot()
	child.Active = false
	r.Accesses[child.ID] = NewAccess(child)
	c := r.Connections["connection"].Snapshot()
	c.CreatedAt = root.ExpiresAt
	r.Connections[c.ID] = NewConnection(c)
	if accesses, connections := Prunable(r, boundary); len(accesses) != 0 || len(connections) != 0 {
		t.Fatal("referenced ancestor deleted", accesses, connections)
	}
	child.ExpiresAt = root.ExpiresAt
	r.Accesses[child.ID] = NewAccess(child)
	accesses, connections := Prunable(r, boundary)
	if len(accesses) != 2 || !slices.Contains(accesses, "root") || !slices.Contains(accesses, "child") || len(connections) != 1 || connections[0] != c.ID {
		t.Fatal("unreferenced expired ancestry retained", accesses, connections)
	}
}

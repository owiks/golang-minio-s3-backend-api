package authstate

// MaxDelegationDepth bounds both persisted ancestry and authorization reads.
// A root has depth zero; up to 32 successive delegations are accepted.
const MaxDelegationDepth = 32

// ValidateLineage checks a detached grant and its ancestors in one repository
// snapshot. A revoked grant remains readable, but its ancestry must still be
// structurally valid. Legacy grants without a parent remain valid roots and do
// not acquire the renewal capability of newly issued service credentials.
func ValidateLineage(access Access, connection Connection, lookup func(string) (Access, Connection, error)) error {
	seen := make(map[string]bool)
	for depth := 0; ; depth++ {
		d, c := access.Snapshot(), connection.Snapshot()
		if depth > MaxDelegationDepth || seen[d.ID] || !ValidAccess(d) || !ValidConnection(c) || d.ConnectionID != c.ID || d.Owner != c.Owner || d.Active && !c.Active {
			return Unavailable()
		}
		seen[d.ID] = true
		if d.ParentID == "" {
			return nil
		}
		if lookup == nil {
			return Unavailable()
		}
		parent, parentConnection, err := lookup(d.ParentID)
		if err != nil {
			return Unavailable()
		}
		p := parent.Snapshot()
		if p.Owner != d.Owner || p.Mode != "bearer" || d.Active && (!p.Active || !p.Policy.Contains(d.Policy) || d.ExpiresAt.After(p.ExpiresAt)) {
			return Unavailable()
		}
		access, connection = parent, parentConnection
	}
}

// ValidateLineages checks a complete repository snapshot, including inactive
// records retained for audit and backup. Adapters must not publish a new grant
// separately from the ancestry changes which make it safe.
func ValidateLineages(records Records) error {
	lookup := func(id string) (Access, Connection, error) {
		a, ok := records.Accesses[id]
		if !ok {
			return Access{}, Connection{}, NotFound()
		}
		c, ok := records.Connections[a.Snapshot().ConnectionID]
		if !ok {
			return Access{}, Connection{}, NotFound()
		}
		return a, c, nil
	}
	for id := range records.Accesses {
		a, c, err := lookup(id)
		if err != nil {
			return Unavailable()
		}
		if err = ValidateLineage(a, c, lookup); err != nil {
			return err
		}
	}
	return nil
}

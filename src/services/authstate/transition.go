package authstate

import "time"

// Records is a detached transaction snapshot. Models own their internal slices.
// Adapters publish Changes and its audit event together, or publish neither.
type Records struct {
	Connections map[string]Connection
	Accesses    map[string]Access
}
type Changes struct {
	Connections []Connection
	Accesses    []Access
	Event       Event
	Result      Result
}

func Transition(records Records, command Command) (Changes, error) {
	return TransitionAt(records, command, time.Now())
}

// TransitionAt evaluates a command at the repository clock after acquiring its
// write lock. Request timestamps cannot extend an expired credential lifetime.
func TransitionAt(records Records, command Command, now time.Time) (Changes, error) {
	actor, ok := records.Accesses[command.Guard.ActorID]
	if !ok {
		return Changes{}, Denied()
	}
	a := actor.Snapshot()
	now = now.UTC()
	actorConnection, ok := records.Connections[a.ConnectionID]
	if !ok || !actorConnection.Snapshot().Active || !actor.Valid(now) || a.Owner != command.Guard.Owner || a.Revision != command.Guard.Revision || a.Generation != command.Guard.Generation || command.Guard.Previous {
		return Changes{}, Denied()
	}
	lookup := func(id string) (Access, Connection, error) {
		v, exists := records.Accesses[id]
		if !exists {
			return Access{}, Connection{}, NotFound()
		}
		c, exists := records.Connections[v.Snapshot().ConnectionID]
		if !exists {
			return Access{}, Connection{}, NotFound()
		}
		return v, c, nil
	}
	if err := ValidateLineage(actor, actorConnection, lookup); err != nil {
		return Changes{}, err
	}
	selfRenew := command.Kind == "renew" && command.TargetID == a.ID && a.Mode == "bearer"
	if command.Kind != "logout" && !selfRenew && !a.Policy.Allows("access:manage") {
		return Changes{}, Denied()
	}
	if command.Event.Owner != a.Owner || command.Event.ActorID != a.ID || command.Event.ID == "" {
		return Changes{}, Invalid()
	}
	changes := Changes{Event: command.Event}
	switch command.Kind {
	case "create":
		child := command.Access.Snapshot()
		connection, found := records.Connections[child.ConnectionID]
		if !found || connection.Snapshot().Owner != a.Owner {
			return Changes{}, NotFound()
		}
		if !connection.Snapshot().Active || connection.Snapshot().Revision != command.ConnectionRevision {
			return Changes{}, Conflict()
		}
		if _, exists := records.Accesses[child.ID]; exists {
			return Changes{}, Conflict()
		}
		if !ValidAccess(child) || child.Mode != "bearer" || !child.Renewable || child.Owner != a.Owner || !child.Policy.Valid() || !a.Policy.Contains(child.Policy) || !ExpiryValid(child.ExpiresAt, now) {
			return Changes{}, Denied()
		}
		if a.Mode == "bearer" && child.ParentID != a.ID || a.Mode == "cookie" && child.ParentID != "" {
			return Changes{}, Denied()
		}
		if err := ValidateLineage(command.Access, connection, lookup); err != nil {
			return Changes{}, Denied()
		}
		active := 0
		for _, access := range records.Accesses {
			if access.Snapshot().ConnectionID == child.ConnectionID && access.Valid(now) {
				active++
			}
		}
		if active >= 256 || len(records.Accesses) >= 4096 {
			return Changes{}, Limit()
		}
		changes.Accesses = []Access{command.Access}
		changes.Result = Result{Access: command.Access, Connection: connection}
	case "revoke_connection", "credentials":
		connection, found := records.Connections[command.TargetID]
		if !found || connection.Snapshot().Owner != a.Owner {
			return Changes{}, NotFound()
		}
		if !a.Policy.Full() {
			return Changes{}, Denied()
		}
		c := connection.Snapshot()
		if !c.Active || c.Revision != command.Revision {
			return Changes{}, Conflict()
		}
		c.Revision++
		if command.Kind == "revoke_connection" {
			c.Active = false
			c.Cipher = Ciphertext{}
		} else {
			if command.SecretVersion != c.SecretVersion+1 || command.Cipher.KeyID == "" || len(command.Cipher.Data) < 28 {
				return Changes{}, Invalid()
			}
			c.SecretVersion = command.SecretVersion
			c.Cipher = command.Cipher
		}
		connection = NewConnection(c)
		changes.Connections = []Connection{connection}
		changes.Result.Connection = connection
		if command.Kind == "revoke_connection" {
			for _, access := range records.Accesses {
				d := access.Snapshot()
				if d.ConnectionID == c.ID && d.Active {
					revokeAccess(&d, now)
					d.Revision++
					changes.Accesses = append(changes.Accesses, NewAccess(d))
				}
			}
			changes.Accesses = cascadeAccesses(records, changes.Accesses, now)
		}
	case "edit", "rotate", "renew", "revoke", "logout":
		target, found := records.Accesses[command.TargetID]
		if !found || target.Snapshot().Owner != a.Owner {
			return Changes{}, NotFound()
		}
		d := target.Snapshot()
		if command.Kind != "logout" && !a.Policy.Contains(d.Policy) {
			return Changes{}, Denied()
		}
		if command.Kind == "logout" && d.ID != a.ID {
			return Changes{}, Denied()
		}
		if command.Kind != "logout" && d.Mode != "bearer" {
			return Changes{}, NotFound()
		}
		if d.Revision != command.Revision {
			return Changes{}, Conflict()
		}
		if command.Kind != "revoke" && command.Kind != "logout" && !target.Valid(now) {
			return Changes{}, Conflict()
		}
		switch command.Kind {
		case "rotate":
			if len(command.Digest) != 64 || command.GraceSeconds < 0 || command.GraceSeconds > 60 {
				return Changes{}, Invalid()
			}
			d.PreviousDigest = ""
			d.PreviousUntil = time.Time{}
			if command.GraceSeconds > 0 {
				d.PreviousDigest = d.Digest
				d.PreviousUntil = EarlierExpiry(now.Add(time.Duration(command.GraceSeconds)*time.Second), d.ExpiresAt)
			}
			d.Digest = command.Digest
			d.Generation++
		case "renew":
			expiry := time.UnixMicro(command.ExpiresAt).UTC()
			if d.ParentID != "" {
				parent, exists := records.Accesses[d.ParentID]
				if !exists || !parent.Valid(now) {
					return Changes{}, Denied()
				}
				expiry = EarlierExpiry(expiry, parent.Snapshot().ExpiresAt)
			}
			if a.Mode == "bearer" && (!selfRenew || !d.Renewable) {
				expiry = EarlierExpiry(expiry, a.ExpiresAt)
			}
			if !ExpiryValid(expiry, now) {
				return Changes{}, Invalid()
			}
			d.ExpiresAt = expiry
		case "edit":
			if command.Name != nil {
				if len(*command.Name) > 128 {
					return Changes{}, Invalid()
				}
				d.Name = *command.Name
			}
			if command.Policy != nil {
				if !command.Policy.Valid() || !d.Policy.Contains(*command.Policy) || !a.Policy.Contains(*command.Policy) {
					return Changes{}, Denied()
				}
				d.Policy = command.Policy.Clone()
			}
		case "revoke", "logout":
			revokeAccess(&d, now)
		}
		d.Revision++
		target = NewAccess(d)
		changes.Accesses = cascadeAccesses(records, []Access{target}, now)
		changes.Result.Access = target
	default:
		return Changes{}, Invalid()
	}
	return changes, nil
}
func ValidateBootstrap(records Records, connection Connection, access Access) error {
	c, a := connection.Snapshot(), access.Snapshot()
	if !ValidConnection(c) || !ValidAccess(a) || a.ParentID != "" || c.Owner == "" || a.Owner != c.Owner || a.ConnectionID != c.ID || !a.Policy.Valid() || !a.Active || !c.Active || c.Revision != 1 || a.Revision != 1 || a.Generation != 1 || len(a.Digest) != 64 || len(c.Cipher.Data) < 28 || c.Cipher.KeyID == "" {
		return Invalid()
	}
	if a.Mode != "cookie" && a.Mode != "bearer" {
		return Invalid()
	}
	if _, ok := records.Connections[c.ID]; ok {
		return Conflict()
	}
	if _, ok := records.Accesses[a.ID]; ok {
		return Conflict()
	}
	count := 0
	for _, other := range records.Connections {
		if other.Snapshot().Owner == c.Owner {
			count++
		}
	}
	if count >= 256 {
		return Limit()
	}
	return nil
}

package commandlog

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
)

// ValidateEntries validates a complete offline snapshot before publishing any
// fence. Duplicate IDs, duplicate command keys and over-capacity batches fail.
func ValidateEntries(entries []Entry) error {
	if len(entries) > MaxRecords {
		return ErrLimit
	}
	ids := map[string]bool{}
	keys := map[memoryKey]bool{}
	actors := map[string]int{}
	total := 0
	for _, entry := range entries {
		key := memoryKey{scope: entry.Scope, hash: entry.KeyHash}
		if !ValidEntry(entry) || ids[entry.ID] || keys[key] {
			return ErrInvalid
		}
		raw, err := json.Marshal(entry)
		if err != nil || len(raw) > MaxEntryBytes {
			return ErrInvalid
		}
		total += len(raw)
		if total > MaxSnapshotBytes {
			return ErrLimit
		}
		ids[entry.ID], keys[key] = true, true
		actors[entry.Scope.ActorID]++
		if actors[entry.Scope.ActorID] > MaxPerActor {
			return ErrLimit
		}
	}
	return nil
}

// SameEntry compares persisted content, including a neutral JSON response whose
// HTML escaping can change while the entry travels through a JSON backup.
func SameEntry(a, b Entry) bool {
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
}

func (m *Memory) Export(ctx context.Context) ([]Entry, error) {
	if ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrUnavailable
	}
	result := make([]Entry, 0, len(m.entries))
	total := 0
	for _, entry := range m.entries {
		raw, err := json.Marshal(entry)
		if err != nil || len(raw) > MaxEntryBytes {
			return nil, ErrUnavailable
		}
		total += len(raw)
		if total > MaxSnapshotBytes {
			return nil, ErrLimit
		}
		result = append(result, entry.Clone())
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (m *Memory) Import(ctx context.Context, entries []Entry) error {
	if err := ValidateEntries(entries); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	incoming := make(map[memoryKey]Entry, len(entries))
	for _, entry := range entries {
		incoming[memoryKey{scope: entry.Scope, hash: entry.KeyHash}] = entry.Clone()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrUnavailable
	}
	for key, existing := range m.entries {
		next, ok := incoming[key]
		if !ok || !SameEntry(existing, next) {
			return ErrConflict
		}
	}
	m.entries = incoming
	return nil
}

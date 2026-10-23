package runtime

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/capabilities"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

const capabilityCacheLimit = 1024
const capabilityProbeLimit = 32

type capabilityOwner struct {
	session, internal, public, revision string
}
type capabilityKey struct {
	owner capabilityOwner
	id    capabilities.Identifier
	scope capabilities.Scope
}
type capabilityEntry struct {
	key    capabilityKey
	result capabilities.Result
}
type capabilityFlight struct{ done chan struct{} }

// CapabilityStore owns a bounded cache and a bounded set of in-flight probes.
// No evidence (including access denial) crosses session or IAM revision keys.
type CapabilityStore struct {
	mu      sync.Mutex
	entries map[capabilityKey]*list.Element
	recent  list.List
	flights map[capabilityKey]*capabilityFlight
	active  chan struct{}
	now     func() time.Time
	closed  bool
}

func NewCapabilityStore() *CapabilityStore {
	return &CapabilityStore{entries: make(map[capabilityKey]*list.Element), flights: make(map[capabilityKey]*capabilityFlight), active: make(chan struct{}, capabilityProbeLimit), now: time.Now}
}

type capabilityView struct {
	store   *CapabilityStore
	owner   capabilityOwner
	expires time.Time
	alive   func() bool
}

func (app *State) CapabilityCache(s *Session) capabilities.Cache       { return app.capabilityView(s) }
func (app *State) CapabilityObserver(s *Session) capabilities.Observer { return app.capabilityView(s) }

// ConditionalCompletionConfigured exposes only an operator-confirmed protocol
// guarantee. It never represents the subject's permission to write an object.
func (c *Clients) ConditionalCompletionConfigured() bool { return c.session.conditionalComplete }
func (app *State) capabilityView(s *Session) *capabilityView {
	return &capabilityView{store: app.Capabilities, owner: capabilityOwner{session: s.id, internal: s.internal.String(), public: s.public.String(), revision: app.AccessRevision(s)}, expires: s.expires, alive: func() bool { return app.Sessions.Has(s.id) }}
}

func (v *capabilityView) Resolve(ctx context.Context, id capabilities.Identifier, scope capabilities.Scope, load func(context.Context) (capabilities.Result, error)) (capabilities.Result, error) {
	s := v.store
	if s == nil {
		return load(ctx)
	}
	key := capabilityKey{owner: v.owner, id: id, scope: scope}
	for {
		if err := ctx.Err(); err != nil {
			return capabilities.Result{}, err
		}
		s.mu.Lock()
		now := s.now()
		if s.closed {
			s.mu.Unlock()
			return capabilities.Result{}, closingError()
		}
		if !authstate.ExpiryValid(v.expires, now) || v.alive != nil && !v.alive() {
			s.mu.Unlock()
			return capabilities.Result{}, expiredSessionError()
		}
		if item := s.entries[key]; item != nil {
			entry := item.Value.(*capabilityEntry)
			if now.Before(entry.result.ExpiresAt) {
				s.recent.MoveToBack(item)
				value := entry.result
				s.mu.Unlock()
				return value, nil
			}
			s.remove(item)
		}
		if flight := s.flights[key]; flight != nil {
			done := flight.done
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return capabilities.Result{}, ctx.Err()
			case <-done:
				continue
			}
		}
		// Admission is non-blocking; callers may retry later. Allocating a
		// waiting flight per arbitrary key would make the cache itself unbounded.
		select {
		case s.active <- struct{}{}:
		default:
			s.mu.Unlock()
			return capabilities.Result{}, apperror.New(429, "CapabilityBusy", messages.RuntimeCapabilityBusy)
		}
		flight := &capabilityFlight{done: make(chan struct{})}
		s.flights[key] = flight
		s.mu.Unlock()
		value, err := load(ctx)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		s.mu.Lock()
		if err == nil && !s.closed && authstate.ExpiryValid(v.expires, s.now()) && (v.alive == nil || v.alive()) {
			// An actual operation may have published newer evidence while this
			// read was in flight. Never replace that newer observation.
			if existing := s.entries[key]; existing == nil || !existing.Value.(*capabilityEntry).result.CheckedAt.After(value.CheckedAt) {
				value = s.put(key, value, v.expires)
			} else {
				value = existing.Value.(*capabilityEntry).result
			}
		}
		delete(s.flights, key)
		close(flight.done)
		<-s.active
		s.mu.Unlock()
		return value, err
	}
}

func (v *capabilityView) Observe(ctx context.Context, observation capabilities.Observation) {
	if v.store == nil || !capabilities.Known(observation.ID) {
		return
	}
	// Validate observed scope as rigorously as a read request. Observers are
	// service ports, not a client-facing mechanism for declaring support.
	if capabilities.Validate(capabilities.Request{Feature: observation.ID, Scope: observation.Scope}) != nil {
		return
	}
	s := v.store
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.closed || !authstate.ExpiryValid(v.expires, now) || v.alive != nil && !v.alive() {
		return
	}
	result := capabilities.Outcome(observation.ID, observation.Scope, "observed_operation", observation.Err, now)
	s.put(capabilityKey{owner: v.owner, id: observation.ID, scope: observation.Scope}, result, v.expires)
}

func (s *CapabilityStore) put(key capabilityKey, value capabilities.Result, sessionExpiry time.Time) capabilities.Result {
	value.ExpiresAt = authstate.EarlierExpiry(value.ExpiresAt, sessionExpiry)
	if item := s.entries[key]; item != nil {
		item.Value.(*capabilityEntry).result = value
		s.recent.MoveToBack(item)
	} else {
		for len(s.entries) >= capabilityCacheLimit {
			s.remove(s.recent.Front())
		}
		s.entries[key] = s.recent.PushBack(&capabilityEntry{key: key, result: value})
	}
	return value
}
func (s *CapabilityStore) remove(item *list.Element) {
	delete(s.entries, item.Value.(*capabilityEntry).key)
	s.recent.Remove(item)
}
func (s *CapabilityStore) Reap(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for item := s.recent.Front(); item != nil; {
		next := item.Next()
		if !now.Before(item.Value.(*capabilityEntry).result.ExpiresAt) {
			s.remove(item)
		}
		item = next
	}
}
func (s *CapabilityStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	clear(s.entries)
	s.recent.Init()
}

// RemoveSession promptly discards evidence after logout. In-flight probes may
// finish under their request context, but cannot expose evidence to another ID.
func (s *CapabilityStore) RemoveSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for item := s.recent.Front(); item != nil; {
		next := item.Next()
		if item.Value.(*capabilityEntry).key.owner.session == id {
			s.remove(item)
		}
		item = next
	}
}

var _ capabilities.Cache = (*capabilityView)(nil)
var _ capabilities.Observer = (*capabilityView)(nil)

package runtime

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads/multipart"
)

// MultipartStore owns a bounded, in-memory operation registry. It never adopts
// uploads from the administrative incomplete-upload listing.
type MultipartStore struct {
	mu         sync.Mutex
	items      map[string]*multipart.Operation
	closed     bool
	ownerAlive func(string) bool
}

func NewMultipartStore(ownerAlive func(string) bool) *MultipartStore {
	return &MultipartStore{items: map[string]*multipart.Operation{}, ownerAlive: ownerAlive}
}
func (s *MultipartStore) Add(o *multipart.Operation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return closingError()
	}
	if s.ownerAlive != nil && !s.ownerAlive(o.Owner()) {
		return expiredSessionError()
	}
	ownerCount := 0
	for _, v := range s.items {
		if v.Owner() == o.Owner() && v.Active() {
			ownerCount++
		}
	}
	if len(s.items) >= 256 || ownerCount >= 16 {
		return apperror.New(429, "MultipartLimit", messages.RuntimeMultipartLimit)
	}
	if s.items[o.ID()] != nil {
		return apperror.New(409, "MultipartExists", messages.RuntimeMultipartExists)
	}
	s.items[o.ID()] = o
	return nil
}
func (s *MultipartStore) Get(id string) *multipart.Operation {
	s.mu.Lock()
	o := s.items[id]
	invalid := s.closed || o != nil && s.ownerAlive != nil && !s.ownerAlive(o.Owner())
	s.mu.Unlock()
	if invalid {
		if o != nil {
			o.Stop()
		}
		return nil
	}
	return o
}

func (s *MultipartStore) candidates(match func(*multipart.Operation) bool) []*multipart.Operation {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := []*multipart.Operation{}
	for _, v := range s.items {
		if match(v) {
			rows = append(rows, v)
		}
	}
	return rows
}
func (s *MultipartStore) remove(o *multipart.Operation) {
	s.mu.Lock()
	if s.items[o.ID()] == o {
		delete(s.items, o.ID())
	}
	s.mu.Unlock()
}

// Discard removes only a definitively failed initiate; unknown outcomes stay owned.
func (s *MultipartStore) Discard(o *multipart.Operation) { s.remove(o) }

func (s *MultipartStore) cleanup(rows []*multipart.Operation, now time.Time, limit int) {
	for _, o := range rows {
		o.Stop()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i, o := range rows {
		if i >= limit || ctx.Err() != nil {
			return
		}
		if !o.PendingCleanup(now) {
			if o.CleanupExhausted() {
				o.ReleaseCleanup()
				s.remove(o)
				slog.Warn("multipart cleanup exhausted; upstream residue may remain", "reason", "CleanupUnconfirmed", "attempts", 3)
			}
			continue
		}
		if o.Cleanup(ctx, now) {
			if reason := o.Snapshot().Reason; reason != "" {
				slog.Warn("multipart cleanup incomplete; upstream residue may remain", "reason", reason)
			}
			o.ReleaseCleanup()
			s.remove(o)
		} else if o.CleanupExhausted() {
			o.ReleaseCleanup()
			s.remove(o)
			slog.Warn("multipart cleanup exhausted; upstream residue may remain", "reason", "CleanupUnconfirmed", "attempts", 3)
		}
	}
}
func (s *MultipartStore) Reap(now time.Time) {
	s.cleanup(s.candidates(func(o *multipart.Operation) bool {
		return o.NeedsCleanup(now) || s.ownerAlive != nil && !s.ownerAlive(o.Owner())
	}), now, 16)
}
func (s *MultipartStore) AbortSession(owner string) {
	s.cleanup(s.candidates(func(o *multipart.Operation) bool { return o.Owner() == owner }), time.Now(), 16)
}
func (s *MultipartStore) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cleanup(s.candidates(func(*multipart.Operation) bool { return true }), time.Now(), 256)
}

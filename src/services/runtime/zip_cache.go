package runtime

import (
	"context"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/archives/zipread"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/coordination"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	objectstate "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/state"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/staging"
)

type storedZIP struct {
	File                *staging.File  `json:"-"`
	Reader              *zipread.Index `json:"-"`
	Revision, SessionID string         `json:"-"`
	Used                time.Time      `json:"-"`
	Refs                int            `json:"-"`
}

// A failed unlink remains owned by the shared budget after cache eviction.
func (z *storedZIP) Close() { _ = z.File.Close() }

type ZIPCache struct {
	mu         sync.Mutex
	entries    map[string]*storedZIP
	loads      coordination.Locks
	temporary  *staging.Budget
	hasSession func(string) bool
	closed     bool
}

func NewZIPCache(temporary *staging.Budget, hasSession func(string) bool) *ZIPCache {
	return &ZIPCache{entries: map[string]*storedZIP{}, temporary: temporary, hasSession: hasSession}
}
func (app *State) CachedZIP(ctx context.Context, s *Session, bucket, key string) (*zipread.Archive, error) {
	return app.ZIPs.Open(ctx, NewClients(s), bucket, key)
}
func (cache *ZIPCache) Open(ctx context.Context, s zipread.ZIPSource, bucket, key string) (*zipread.Archive, error) {
	cache.mu.Lock()
	closed := cache.closed
	cache.mu.Unlock()
	if closed {
		return nil, closingError()
	}
	if !cache.hasSession(s.SessionID()) {
		return nil, expiredSessionError()
	}
	info, err := s.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return nil, err
	}
	rev := objectstate.DeletionRevision(objectstate.DeletionSnapshot{ETag: info.ETag, Size: info.Size, Modified: info.LastModified, Version: info.VersionID})
	id := s.SessionID() + "\x00" + bucket + "\x00" + key + "\x00" + rev
	unlock, err := cache.loads.Acquire(ctx, id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	cache.mu.Lock()
	if cache.closed {
		cache.mu.Unlock()
		return nil, apperror.New(503, "ServerClosing", messages.RuntimeServerClosing)
	}
	// A request may have authenticated before logout while another lease keeps
	// this entry alive. Hits and new publications use the same session fence.
	if !cache.hasSession(s.SessionID()) {
		cache.mu.Unlock()
		return nil, expiredSessionError()
	}
	if cached := cache.entries[id]; cached != nil {
		cached.Refs++
		cached.Used = time.Now()
		cache.mu.Unlock()
		return cached.Reader.Borrow(cached.Revision, cache.release(id, cached)), nil
	}

	// Reclaim idle cache before reserving disk bytes for a new archive.
	for len(cache.entries) >= 4 || cache.temporary.Available() < info.Size {
		var oldestID string
		var oldest *storedZIP
		for key, entry := range cache.entries {
			if entry.Refs == 0 && (oldest == nil || entry.Used.Before(oldest.Used)) {
				oldestID = key
				oldest = entry
			}
		}
		if oldest == nil {
			break
		}
		oldest.Close()
		delete(cache.entries, oldestID)
	}
	cache.mu.Unlock()
	file, reader, actual, err := zipread.LoadStoredZIP(ctx, s, bucket, key, cache.temporary)
	if err != nil {
		return nil, err
	}
	entry := &storedZIP{File: file, Reader: reader, Revision: actual, SessionID: s.SessionID(), Refs: 1, Used: time.Now()}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.closed {
		entry.Close()
		return nil, apperror.New(503, "ServerClosing", messages.RuntimeServerClosing)
	}
	if !cache.hasSession(s.SessionID()) {
		entry.Close()
		return nil, expiredSessionError()
	}
	if actual != rev {
		entry.Close()
		return nil, apperror.New(409, "ZIPChanged", messages.ArchivesZIPChangedReader)
	}
	if cached := cache.entries[id]; cached != nil {
		entry.Close()
		cached.Refs++
		return cached.Reader.Borrow(cached.Revision, cache.release(id, cached)), nil
	}
	if len(cache.entries) >= 4 {
		var oldestID string
		var oldest *storedZIP
		for ticket, cached := range cache.entries {
			if cached.Refs == 0 && (oldest == nil || cached.Used.Before(oldest.Used)) {
				oldestID = ticket
				oldest = cached
			}
		}
		if oldest == nil {
			entry.Close()
			return nil, apperror.New(429, "ZIPBusy", messages.RuntimeZIPBusy)
		}
		oldest.Close()
		delete(cache.entries, oldestID)
	}
	cache.entries[id] = entry
	return entry.Reader.Borrow(entry.Revision, cache.release(id, entry)), nil
}

func (cache *ZIPCache) release(id string, entry *storedZIP) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			cache.mu.Lock()
			defer cache.mu.Unlock()
			entry.Refs--
			entry.Used = time.Now()
			if entry.Refs == 0 && (cache.closed || !cache.hasSession(entry.SessionID)) {
				delete(cache.entries, id)
				entry.Close()
			}
		})
	}
}

// Reap never closes a file held by a reader. Its final release handles cleanup.
func (cache *ZIPCache) Reap(now time.Time) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	for id, entry := range cache.entries {
		if entry.Refs == 0 && (now.Sub(entry.Used) > 15*time.Minute || !cache.hasSession(entry.SessionID)) {
			entry.Close()
			delete(cache.entries, id)
		}
	}
}
func (cache *ZIPCache) Close() {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.closed = true
	for id, entry := range cache.entries {
		if entry.Refs == 0 {
			entry.Close()
			delete(cache.entries, id)
		}
	}
}

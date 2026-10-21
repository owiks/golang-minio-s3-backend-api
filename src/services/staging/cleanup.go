package staging

import (
	"errors"
	"log/slog"
	"sort"
	"time"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

const (
	cleanupBatch     = 32
	maxPendingFiles  = 256
	shutdownAttempts = 3
	cleanupDelay     = time.Minute
	maxCleanupDelay  = 5 * time.Minute
)

var ErrCleanupPending = errors.New("temporary file cleanup remains pending")

type cleanupEntry struct {
	file     *File
	next     time.Time
	id       uint64
	attempts int
	busy     bool
}

// CleanupStats contains counts only, never filesystem paths or raw OS errors.
// Bytes are retained reservations, not measured filesystem usage.
type CleanupStats struct {
	Files int
	Bytes int64
}

func closingError() error {
	return apperror.New(503, "ServerClosing", messages.RuntimeServerClosing)
}

func (b *Budget) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

func (b *Budget) retain(f *File) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending[f] != nil {
		return false
	}
	if b.pending == nil {
		b.pending = map[*File]*cleanupEntry{}
	}
	b.nextID++
	b.pending[f] = &cleanupEntry{file: f, next: time.Now().Add(cleanupDelay), id: b.nextID}
	return true
}

func (b *Budget) forget(f *File) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pending, f)
}

func (b *Budget) PendingCleanup() CleanupStats {
	b.mu.Lock()
	defer b.mu.Unlock()
	stats := CleanupStats{Files: len(b.pending)}
	for f := range b.pending {
		stats.Bytes += f.limit
	}
	return stats
}

// Reap retries at most one batch, oldest due work first. Persistent failures
// back off to five minutes without losing ownership or releasing their bytes.
func (b *Budget) Reap(now time.Time) {
	b.retry(now, false)
}

func (b *Budget) retry(now time.Time, shutdown bool) {
	b.mu.Lock()
	entries := make([]*cleanupEntry, 0, len(b.pending))
	for _, entry := range b.pending {
		if !entry.busy && (shutdown || !now.Before(entry.next)) {
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].next.Equal(entries[j].next) {
			return entries[i].id < entries[j].id
		}
		return entries[i].next.Before(entries[j].next)
	})
	if !shutdown && len(entries) > cleanupBatch {
		entries = entries[:cleanupBatch]
	}
	for _, entry := range entries {
		entry.busy = true
	}
	b.mu.Unlock()

	// Never hold Budget.mu while acquiring File.mu: Close hands off in the
	// opposite direction. A concurrent successful Close removes the entry.
	for _, entry := range entries {
		err := entry.file.Close()
		b.mu.Lock()
		if b.pending[entry.file] == entry {
			entry.busy = false
			if err != nil {
				entry.attempts = min(entry.attempts+1, 4)
				delay := min(cleanupDelay*time.Duration(1<<entry.attempts), maxCleanupDelay)
				entry.next = now.Add(delay)
			}
		}
		b.mu.Unlock()
	}
}

// Close stops new reservations and retries every pending file, even if its
// backoff has not elapsed. Each file gets at most three unlink attempts. It does
// not close resources still owned by active operations. Failed/late releases
// remain accounted for; Close can be called again to retry them.
func (b *Budget) Close() error {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.retry(time.Now(), true)
	stats := b.PendingCleanup()
	if stats.Files != 0 {
		slog.Warn("temporary cleanup incomplete at shutdown", "pending_files", stats.Files, "pending_bytes", stats.Bytes)
		return ErrCleanupPending
	}
	return nil
}

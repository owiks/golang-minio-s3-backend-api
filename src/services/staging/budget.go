// Package staging owns the shared byte budget for temporary files.
package staging

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

type Budget struct {
	mu        sync.Mutex
	max, used int64
	closed    bool
	pending   map[*File]*cleanupEntry
	nextID    uint64
}

func New(max int64) *Budget { return &Budget{max: max, pending: map[*File]*cleanupEntry{}} }
func full() error {
	return apperror.New(429, "TemporaryStorageFull", messages.StagingTemporaryStorageFull)
}
func (b *Budget) Reserve(size int64) (func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, closingError()
	}
	// Failed zero-byte files must not bypass cleanup backpressure forever.
	if size < 0 || size > b.max-b.used || len(b.pending) >= maxPendingFiles {
		return nil, full()
	}
	b.used += size
	var once sync.Once
	return func() { once.Do(func() { b.mu.Lock(); b.used -= size; b.mu.Unlock() }) }, nil
}
func (b *Budget) Used() int64 { b.mu.Lock(); defer b.mu.Unlock(); return b.used }

// File exposes bounded I/O, never the raw file: io.Copy must not bypass Write.
// A failed unlink transfers cleanup to Budget and retains its reservation.
type File struct {
	file    *os.File
	owner   *Budget
	release func()
	limit   int64
	mu      sync.RWMutex
	closed  bool
}

func (b *Budget) Create(size int64, pattern string) (*File, error) {
	release, err := b.Reserve(size)
	if err != nil {
		return nil, err
	}
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		release()
		return nil, err
	}
	file := &File{file: f, owner: b, release: release, limit: size}
	if b.isClosed() {
		_ = file.Close() // Failed cleanup is retained by the owner, not the caller.
		return nil, closingError()
	}
	return file, nil
}
func (f *File) Name() string { return f.file.Name() }
func (f *File) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.file.Read(p)
}
func (f *File) ReadAt(p []byte, off int64) (int, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.file.ReadAt(p, off)
}
func (f *File) Seek(off int64, whence int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.file.Seek(off, whence)
}
func (f *File) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	off, err := f.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	if off < 0 || int64(len(p)) > f.limit-off {
		return 0, full()
	}
	return f.file.Write(p)
}
func (f *File) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	closeErr := f.file.Close()
	removeErr := os.Remove(f.Name())
	// Late releases after shutdown still get bounded retries. Active readers
	// remain owned by their caller until Close, never by the cleanup queue.
	if f.owner.isClosed() {
		for attempt := 1; attempt < shutdownAttempts && removeErr != nil && !errors.Is(removeErr, os.ErrNotExist); attempt++ {
			removeErr = os.Remove(f.Name())
		}
	}
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		if f.owner.retain(f) {
			stats := f.owner.PendingCleanup()
			slog.Warn("temporary file cleanup deferred; reservation retained", "pending_files", stats.Files, "pending_bytes", stats.Bytes)
		}
		return removeErr
	}
	f.closed = true
	f.owner.forget(f)
	f.release()
	if errors.Is(closeErr, os.ErrClosed) {
		return nil
	}
	return closeErr
}

func (b *Budget) Available() int64 { b.mu.Lock(); defer b.mu.Unlock(); return b.max - b.used }

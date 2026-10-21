package folders

import (
	"context"
	"io"
	"sync"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
)

type SharedFileInfo struct {
	Size, Start, End               int64
	Status                         int
	ContentType, Disposition, Name string
	Media                          bool
}

// SharedRead owns its context, SDK reader and admission slot. Only StreamTo
// consumes the reader; Close cancels active work without waiting on its mutex.
type SharedRead struct {
	mu                        sync.Mutex
	started, finished, closed bool
	ctx                       context.Context
	cancel                    context.CancelFunc
	object                    *minio.Object
	release                   func()
	info                      SharedFileInfo
}

func (s *SharedRead) Context() context.Context { return s.ctx }
func (s *SharedRead) Summary() SharedFileInfo  { return s.info }

func (s *SharedRead) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	object, release := s.object, s.release
	if s.started && !s.finished {
		object, release = nil, nil // The active consumer retains resource ownership.
	} else {
		s.object, s.release = nil, nil
	}
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if object != nil {
		_ = object.Close()
	}
	if release != nil {
		release()
	}
}

func (s *SharedRead) finish() {
	s.mu.Lock()
	object := s.object
	s.object = nil
	s.mu.Unlock()
	if object != nil {
		_ = object.Close()
	}
	s.mu.Lock()
	s.finished = true
	var release func()
	if s.closed {
		release, s.release = s.release, nil
	}
	s.mu.Unlock()
	if release != nil {
		release()
	}
	// Normal completion leaves context/admission cleanup to Close. The HTTP
	// owner must detach its cancellation watcher before releasing the lease.
}

func (s *SharedRead) StreamTo(target io.Writer) (int64, error) {
	s.mu.Lock()
	if s.started || s.closed || s.object == nil {
		s.mu.Unlock()
		return 0, apperror.New(409, "SharedReadClosed", "Чтение уже запускалось или закрыто. Получите новую ссылку.")
	}
	s.started = true
	object := s.object
	s.mu.Unlock()
	defer s.finish()
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	return io.Copy(target, object)
}

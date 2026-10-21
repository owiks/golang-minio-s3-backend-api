package objects

import (
	"context"
	"slices"
	"sync"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/result"
)

// ChecksumRun owns its prepared scope, references and S3 reader. Only one
// consumer may stream it; UI summaries are detached from the live operation.
type ChecksumRun struct {
	mu                        sync.Mutex
	started, finished, closed bool
	calculate                 bool
	ctx                       context.Context
	cancel                    context.CancelFunc
	client                    StreamClient
	bucket, key, expected     string
	summary                   ChecksumInfo
	info                      minio.ObjectInfo
	object                    *minio.Object
	first                     []byte
	n                         int
	readErr                   error
}

func (c *ChecksumRun) Summary() ChecksumInfo {
	value := c.summary
	value.Checksums = slices.Clone(value.Checksums)
	return value
}

// Close cancels before touching the SDK object: its Close waits for Read's
// mutex. An active consumer owns final cleanup and must be allowed to unwind.
func (c *ChecksumRun) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	object := c.object
	if c.started && !c.finished {
		object = nil
	} else {
		c.object, c.first = nil, nil
	}
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if object != nil {
		_ = object.Close()
	}
}

func (c *ChecksumRun) finish() {
	c.mu.Lock()
	object, cancel := c.object, c.cancel
	c.object, c.first = nil, nil
	c.finished, c.closed = true, true
	c.mu.Unlock()
	cancel()
	if object != nil {
		_ = object.Close()
	}
}

// Stream uses only the validated preparation inputs. Rejected consumers never
// emit into another consumer's stream or repeat any S3 work.
func (c *ChecksumRun) Stream(ctx context.Context, emit func(result.StreamEvent[ChecksumResult]) bool) error {
	c.mu.Lock()
	var err error
	switch {
	case c.started:
		err = apperror.New(409, "ChecksumStarted", messages.ObjectsChecksumStarted)
	case c.closed:
		err = apperror.New(409, "ChecksumClosed", messages.ObjectsChecksumClosed)
	case !c.calculate:
		err = apperror.New(409, "ChecksumNotPrepared", messages.ObjectsChecksumNotPrepared)
	default:
		c.started = true
	}
	c.mu.Unlock()
	if err != nil {
		return err
	}
	defer c.finish()
	if ctx.Err() != nil {
		c.cancel()
	}
	stop := context.AfterFunc(ctx, c.cancel)
	defer stop()
	terminal := false
	c.stream(ctx, func(event result.StreamEvent[ChecksumResult]) bool {
		ok := emit(event)
		if ok && (event.Result != nil || event.Error != nil) {
			terminal = true
		}
		return ok
	})
	if terminal {
		return nil
	}
	return c.ctx.Err()
}

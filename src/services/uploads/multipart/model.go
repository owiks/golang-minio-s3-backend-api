package multipart

import (
	"context"
	"sync"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/coordination"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/objects/write"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads/integrity"
)

const (
	PartSize  int64 = 16 * 1024 * 1024
	MaxParts        = 320
	TTL             = time.Hour
	SignedTTL       = 2 * time.Minute
	markerKey       = "minio-web-upload-operation"
)

type CreateRequest struct {
	Bucket      string            `json:"bucket" validate:"required,maxBytes=63"`
	Key         string            `json:"key" validate:"required,maxBytes=1024"`
	Revision    string            `json:"revision" validate:"required,maxBytes=1024"`
	Size        int64             `json:"size"`
	ContentType string            `json:"contentType,omitempty" validate:"maxBytes=256"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Checksum    *integrity.Spec   `json:"checksum,omitempty"`
}

type PartRequest struct {
	Checksum integrity.Spec `json:"checksum"`
}

type Part struct {
	Number   int    `json:"number"`
	Size     int64  `json:"size"`
	ETag     string `json:"etag"`
	Checksum string `json:"checksumSHA256"`
}

type Snapshot struct {
	ID        string                 `json:"id"`
	Bucket    string                 `json:"bucket"`
	Key       string                 `json:"key"`
	Size      int64                  `json:"size"`
	PartSize  int64                  `json:"partSize"`
	PartCount int                    `json:"partCount"`
	State     string                 `json:"state"`
	Parts     []Part                 `json:"parts"`
	CreatedAt time.Time              `json:"createdAt"`
	ExpiresAt time.Time              `json:"expiresAt"`
	Reason    string                 `json:"reason,omitempty"`
	Receipt   *uploads.UploadReceipt `json:"receipt,omitempty"`
}

type SignedPart struct {
	ID              string            `json:"id"`
	PartNumber      int               `json:"partNumber"`
	URL             string            `json:"url"`
	Method          string            `json:"method"`
	Headers         map[string]string `json:"headers"`
	ExpiresAt       time.Time         `json:"expiresAt"`
	Size            int64             `json:"size"`
	SizeEnforcement string            `json:"sizeEnforcement"`
	Verification    string            `json:"verification"`
}

// Operation owns all mutable state; callers only receive detached snapshots.
// The action lock is cancellable and serializes part/complete/abort. No request
// context is retained: cleanup has its own bounded context and native client.
type Operation struct {
	mu                                                           sync.Mutex
	locks                                                        coordination.Locks
	id, owner, bucket, key, contentType, uploadID, state, reason string
	size                                                         int64
	count                                                        int
	created, expires                                             time.Time
	destination                                                  objectwrite.FileState
	metadata                                                     map[string]string
	checksum                                                     *integrity.Spec
	sealed                                                       map[int]string
	parts                                                        []Part
	receipt                                                      *uploads.UploadReceipt
	cleanup                                                      Client
	cleanupAttempts                                              int
	nextCleanup                                                  time.Time
	cancel                                                       context.CancelFunc
	stopped                                                      bool
}

func (o *Operation) ID() string           { return o.id }
func (o *Operation) Owner() string        { return o.owner }
func (o *Operation) ExpiresAt() time.Time { return o.expires }
func (o *Operation) Active() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.state != "completed" && o.state != "aborted" && o.state != "failed" && o.state != "expired"
}
func (o *Operation) NeedsCleanup(now time.Time) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.stopped || !o.expires.After(now)
}
func (o *Operation) CleanupExhausted() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cleanupAttempts >= 3 && o.reason == "CleanupUnconfirmed"
}

// ReleaseCleanup drops credentials after the bounded policy has stopped. It
// does not assert that the upstream multipart was removed.
func (o *Operation) ReleaseCleanup() { o.mu.Lock(); o.cleanup = nil; o.mu.Unlock() }
func (o *Operation) Snapshot() Snapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.snapshotLocked()
}
func (o *Operation) snapshotLocked() Snapshot {
	s := Snapshot{ID: o.id, Bucket: o.bucket, Key: o.key, Size: o.size, PartSize: PartSize, PartCount: o.count, State: o.state, Parts: append([]Part{}, o.parts...), CreatedAt: o.created, ExpiresAt: o.expires, Reason: o.reason}
	if o.receipt != nil {
		v := *o.receipt
		if v.Checksum != nil {
			e := *v.Checksum
			e.Storage = append([]integrity.Spec(nil), e.Storage...)
			if e.Expected != nil {
				a := *e.Expected
				e.Expected = &a
			}
			if e.Computed != nil {
				a := *e.Computed
				e.Computed = &a
			}
			v.Checksum = &e
		}
		s.Receipt = &v
	}
	return s
}

func (o *Operation) partSize(n int) int64 {
	if n == o.count {
		return o.size - int64(n-1)*PartSize
	}
	return PartSize
}

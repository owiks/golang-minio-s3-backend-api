package multipart

import (
	"context"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	objectwrite "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/write"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads/integrity"
)

func Complete(ctx context.Context, store Store, c Client, id string) (Snapshot, error) {
	o, release, err := acquire(ctx, store, c, id)
	if err != nil {
		return Snapshot{}, err
	}
	defer release()
	ctx, cancel := o.actionContext(ctx)
	defer cancel()
	o.mu.Lock()
	state := o.state
	o.mu.Unlock()
	if state == "completed" {
		return o.Snapshot(), nil
	}
	if state == "complete_uncertain" {
		o.reconcile(ctx, c)
		return o.Snapshot(), nil
	}
	if state != "active" {
		return Snapshot{}, transitionError()
	}
	if err = canWrite(c, o.bucket, o.key); err != nil {
		return Snapshot{}, err
	}
	parts, err := o.serverParts(ctx, c)
	if err != nil {
		return Snapshot{}, err
	}
	if len(parts) != o.count {
		return Snapshot{}, apperror.New(409, "MissingUploadParts", messages.UploadsMissingUploadParts)
	}
	for i, p := range parts {
		if p.Number != i+1 {
			return Snapshot{}, invalidParts()
		}
	}
	checksum := composite(parts)
	if o.checksum != nil && o.checksum.Value != checksum {
		return Snapshot{}, invalid("ChecksumMismatch", messages.UploadsTheSHA256COMPOSITEChecksumOfTheParts)
	}
	current, err := objectwrite.DestinationState(ctx, c, o.bucket, o.key)
	if err != nil {
		return Snapshot{}, err
	}
	opts, err := objectwrite.WriteOptions(current, o.destination.Revision, o.contentType)
	if err != nil {
		return Snapshot{}, err
	}
	// Native preconditions are selected from the original checked destination,
	// then sent on Complete itself. The second HEAD is only an early conflict
	// check; it never replaces the precondition against an external writer.
	opts, err = objectwrite.WriteOptions(o.destination, o.destination.Revision, o.contentType)
	if err != nil {
		return Snapshot{}, err
	}
	opts.UserMetadata = map[string]string{"x-amz-checksum-sha256": checksum, "x-amz-checksum-type": "COMPOSITE"}
	complete := make([]minio.CompletePart, len(parts))
	for i, p := range parts {
		complete[i] = minio.CompletePart{PartNumber: p.Number, ETag: p.ETag, ChecksumSHA256: p.Checksum}
	}
	o.mu.Lock()
	o.state = "completing"
	o.parts = parts
	o.mu.Unlock()
	info, err := c.CompleteMultipartUpload(ctx, o.bucket, o.key, o.uploadID, complete, opts)
	if err != nil {
		observe(ctx, c, "complete", o.bucket, o.key, err)
		o.mu.Lock()
		if definitive(err) {
			o.state = "active"
			o.reason = ""
		} else {
			o.state = "complete_uncertain"
			o.reason = "CompleteOutcomeUnknown"
		}
		o.mu.Unlock()
		if definitive(err) {
			return Snapshot{}, objectwrite.FileWriteError(err)
		}
		// Reconciliation is a separate read with its own request budget. A
		// disconnected client's exhausted context must not be reused for retries.
		return o.Snapshot(), nil
	}
	if info.ETag == "" || info.Bucket != "" && info.Bucket != o.bucket || info.Key != "" && info.Key != o.key {
		observe(ctx, c, "complete", o.bucket, o.key, apperror.New(502, "InvalidCompleteResponse", messages.UploadsInvalidCompleteResponse))
		o.mu.Lock()
		o.state = "complete_uncertain"
		o.reason = "InvalidCompleteResponse"
		o.mu.Unlock()
		return o.Snapshot(), nil
	}
	observe(ctx, c, "complete", o.bucket, o.key, nil)
	o.confirm(info, checksum)
	return o.Snapshot(), nil
}

func (o *Operation) confirm(info minio.UploadInfo, checksum string) {
	computed := integrity.Spec{Algorithm: "SHA256", Encoding: "base64", Value: checksum, Type: "COMPOSITE", Scope: "OBJECT"}
	receipt := uploads.UploadReceipt{OK: true, ETag: info.ETag, VersionID: info.VersionID, Size: o.size}
	receipt.Checksum = integrity.FromUpload(info, o.checksum, &computed, "native_part_sha256_and_conditional_complete")
	if receipt.Checksum != nil && receipt.Checksum.Verification == "storage_mismatch" {
		receipt.OK = false
		receipt.WriteState = "committed"
	}
	o.mu.Lock()
	o.receipt = &receipt
	o.state = "completed"
	o.reason = ""
	if !receipt.OK {
		o.reason = "ChecksumMismatchAfterCommit"
	}
	o.mu.Unlock()
}

func (o *Operation) reconcile(ctx context.Context, c Client) {
	// An opaque per-operation marker plus expected size AND persisted SHA256
	// COMPOSITE distinguishes this outcome from an unrelated object at the key.
	// Missing checksum/marker never turns a lost Complete response into success.
	info, err := c.StatObject(ctx, o.bucket, o.key, minio.StatObjectOptions{Checksum: true})
	if err != nil || info.Size != o.size || info.ETag == "" {
		return
	}
	marker := info.UserMetadata[markerKey]
	if marker == "" {
		marker = info.Metadata.Get("X-Amz-Meta-" + markerKey)
	}
	if marker != o.id {
		return
	}
	want := composite(o.parts)
	if info.ChecksumSHA256 != want {
		return
	}
	if info.ChecksumMode != "" && info.ChecksumMode != "COMPOSITE" {
		return
	}
	o.confirm(minio.UploadInfo{ETag: info.ETag, VersionID: info.VersionID, Size: info.Size, ChecksumSHA256: info.ChecksumSHA256, ChecksumMode: info.ChecksumMode}, want)
	o.mu.Lock()
	if o.receipt != nil && o.receipt.Checksum != nil {
		o.receipt.Checksum.StorageSource = "object_metadata_readback"
	}
	o.mu.Unlock()
}

func Abort(ctx context.Context, store Store, c Client, id string) (Snapshot, error) {
	o, release, err := acquire(ctx, store, c, id)
	if err != nil {
		return Snapshot{}, err
	}
	defer release()
	ctx, cancel := o.actionContext(ctx)
	defer cancel()
	o.mu.Lock()
	state := o.state
	o.mu.Unlock()
	if state == "aborted" {
		return o.Snapshot(), nil
	}
	if state == "completed" || state == "complete_uncertain" || state == "create_uncertain" || state == "failed" {
		return Snapshot{}, transitionError()
	}
	if state != "active" && state != "abort_uncertain" {
		return Snapshot{}, transitionError()
	}
	o.mu.Lock()
	o.state = "aborting"
	o.mu.Unlock()
	err = c.AbortMultipartUpload(ctx, o.bucket, o.key, o.uploadID)
	observe(ctx, c, "abort", o.bucket, o.key, err)
	o.mu.Lock()
	defer o.mu.Unlock()
	if err != nil && minio.ToErrorResponse(err).Code != "NoSuchUpload" {
		o.state = "abort_uncertain"
		o.reason = "AbortOutcomeUnknown"
		return o.snapshotLocked(), nil
	}
	o.state = "aborted"
	o.reason = ""
	return o.snapshotLocked(), nil
}

// Stop prevents new work and cancels the current API transfer. It cannot revoke
// a URL already issued to a browser; late S3 requests remain bound to the same
// upload, part number and sealed checksum, and cannot bypass conditional commit.
func (o *Operation) Stop() {
	o.mu.Lock()
	o.stopped = true
	if o.cancel != nil {
		o.cancel()
	}
	o.mu.Unlock()
}

// Cleanup is an explicit bounded policy attempt, not a transport retry. Unknown
// creates have no upload ID and cannot be guessed or adopted from admin lists.
// Exhausted attempts are reported as unconfirmed residue; the runtime may then
// release this in-memory record and its credentials without claiming removal.
func (o *Operation) Cleanup(ctx context.Context, now time.Time) bool {
	o.Stop()
	release, err := o.locks.Acquire(ctx, "action")
	if err != nil {
		return false
	}
	defer release()
	o.mu.Lock()
	state := o.state
	cleanupClient := o.cleanup
	if cleanupClient == nil {
		o.mu.Unlock()
		return true
	}
	if state == "completed" || state == "aborted" || state == "failed" {
		o.mu.Unlock()
		return true
	}
	if o.uploadID == "" {
		o.state = "expired"
		o.reason = "UnidentifiedUploadMayRemain"
		o.mu.Unlock()
		return true
	}
	if o.cleanupAttempts >= 3 || now.Before(o.nextCleanup) {
		o.mu.Unlock()
		return false
	}
	o.cleanupAttempts++
	o.nextCleanup = now.Add(time.Minute)
	o.state = "expired"
	o.reason = ""
	o.mu.Unlock()
	if state == "complete_uncertain" {
		o.reconcile(ctx, cleanupClient)
		o.mu.Lock()
		done := o.state == "completed"
		o.mu.Unlock()
		if done {
			return true
		}
	}
	err = cleanupClient.AbortMultipartUpload(ctx, o.bucket, o.key, o.uploadID)
	observe(ctx, cleanupClient, "abort", o.bucket, o.key, err)
	o.mu.Lock()
	defer o.mu.Unlock()
	if err == nil || minio.ToErrorResponse(err).Code == "NoSuchUpload" {
		o.state = "expired"
		o.reason = ""
		return true
	}
	o.reason = "CleanupUnconfirmed"
	return false
}

// PendingCleanup reports immutable scheduling facts. A hard cap prevents an
// unhealthy upstream from turning minute reaping into unlimited network work.
func (o *Operation) PendingCleanup(now time.Time) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cleanupAttempts < 3 && !now.Before(o.nextCleanup)
}

func IsUncertain(s Snapshot) bool { return strings.HasSuffix(s.State, "_uncertain") }

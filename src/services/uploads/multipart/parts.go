package multipart

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/uploads/integrity"
)

func partNumber(n int) string { return strconv.Itoa(n) }

func partChecksum(s integrity.Spec) (integrity.Spec, error) {
	s, err := integrity.Normalize(s, "PART")
	if err != nil {
		return s, err
	}
	if s.Algorithm != "SHA256" || s.Type != "FULL_OBJECT" {
		return s, apperror.New(422, "UnsupportedChecksumMode", messages.UploadsUnsupportedChecksumModeParts)
	}
	return s, nil
}

// serverParts treats S3 as the source of truth and validates pagination before
// exposing the bounded detached result. A browser's ETag is never accepted.
func (o *Operation) serverParts(ctx context.Context, c Client) (result []Part, err error) {
	defer func() { observe(ctx, c, "list", o.bucket, o.key, err) }()
	parts := []Part{}
	marker := 0
	for page := 0; page < 5; page++ {
		v, err := c.ListObjectParts(ctx, o.bucket, o.key, o.uploadID, marker, 100)
		if err != nil {
			return nil, err
		}
		if v.Bucket != "" && v.Bucket != o.bucket || v.Key != "" && v.Key != o.key || v.UploadID != "" && v.UploadID != o.uploadID {
			return nil, invalidParts()
		}
		last := marker
		for _, p := range v.ObjectParts {
			if len(parts) >= o.count || p.PartNumber <= last || p.PartNumber > o.count || p.ETag == "" || len(p.ETag) > 1024 {
				return nil, invalidParts()
			}
			if p.Size != o.partSize(p.PartNumber) {
				return nil, apperror.New(409, "UploadedPartSizeMismatch", messages.UploadsUploadedPartSizeMismatch)
			}
			spec, err := partChecksum(integrity.Spec{Algorithm: "SHA256", Encoding: "base64", Type: "FULL_OBJECT", Scope: "PART", Value: p.ChecksumSHA256})
			if err != nil {
				return nil, apperror.New(409, "PartChecksumUnavailable", messages.UploadsPartChecksumUnavailable)
			}
			if expected := o.sealed[p.PartNumber]; expected == "" || expected != spec.Value {
				return nil, apperror.New(409, "PartChanged", messages.UploadsPartChanged)
			}
			parts = append(parts, Part{Number: p.PartNumber, Size: p.Size, ETag: p.ETag, Checksum: spec.Value})
			last = p.PartNumber
		}
		if !v.IsTruncated {
			return parts, nil
		}
		if v.NextPartNumberMarker <= marker || v.NextPartNumberMarker != last || len(v.ObjectParts) == 0 {
			return nil, invalidParts()
		}
		marker = v.NextPartNumberMarker
	}
	return nil, invalidParts()
}

func invalidParts() error {
	return apperror.New(502, "InvalidMultipartParts", messages.UploadsInvalidMultipartParts)
}

func Inspect(ctx context.Context, store Store, c Client, id string) (Snapshot, error) {
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
	if state == "active" {
		parts, err := o.serverParts(ctx, c)
		if err != nil {
			return Snapshot{}, err
		}
		o.mu.Lock()
		o.parts = parts
		o.mu.Unlock()
	} else if state == "complete_uncertain" {
		o.reconcile(ctx, c)
	}
	return o.Snapshot(), nil
}

func PutPart(ctx context.Context, store Store, c Client, id string, n int, size int64, spec integrity.Spec, body io.Reader) (Snapshot, error) {
	spec, err := partChecksum(spec)
	if err != nil {
		return Snapshot{}, err
	}
	o, release, err := acquire(ctx, store, c, id)
	if err != nil {
		return Snapshot{}, err
	}
	defer release()
	ctx, cancel := o.actionContext(ctx)
	defer cancel()
	// The HTTP body is an owned stream when it implements Close. Cancel must
	// interrupt its blocked read as well as the upstream write; a request error
	// must not leave the SDK transport waiting on a slow original reader.
	if closer, ok := body.(io.Closer); ok {
		stop := context.AfterFunc(ctx, func() {
			if interruptible, ok := body.(interface{ InterruptRead() }); ok {
				interruptible.InterruptRead()
			}
			_ = closer.Close()
		})
		defer func() { stop(); _ = closer.Close() }()
	}
	if n < 1 || n > o.count || size != o.partSize(n) {
		return Snapshot{}, invalid("InvalidPartSize", messages.UploadsThePartNumberAndExactSizeMust)
	}
	if err = canWrite(c, o.bucket, o.key); err != nil {
		return Snapshot{}, err
	}
	o.mu.Lock()
	state := o.state
	previous := o.sealed[n]
	o.mu.Unlock()
	if state != "active" {
		return Snapshot{}, transitionError()
	}
	if previous != "" && previous != spec.Value {
		return Snapshot{}, apperror.New(409, "PartContentConflict", messages.UploadsPartContentConflict)
	}
	parts, err := o.serverParts(ctx, c)
	if err != nil {
		return Snapshot{}, err
	}
	for _, p := range parts {
		if p.Number == n {
			o.mu.Lock()
			o.parts = parts
			o.mu.Unlock()
			return o.Snapshot(), nil
		}
	}
	o.mu.Lock()
	o.sealed[n] = spec.Value
	o.mu.Unlock()
	digest, _ := base64.StdEncoding.DecodeString(spec.Value)
	reader := &partReader{reader: body, left: size, hash: sha256.New()}
	_, err = c.PutObjectPart(ctx, o.bucket, o.key, o.uploadID, n, reader, size, minio.PutObjectPartOptions{Sha256Hex: hex.EncodeToString(digest), CustomHeader: http.Header{"X-Amz-Checksum-Sha256": {spec.Value}}})
	observe(ctx, c, "part", o.bucket, o.key, err)
	if err != nil {
		if code := minio.ToErrorResponse(err).Code; code == "BadDigest" || code == "XAmzContentSHA256Mismatch" || code == "ContentSHA256Mismatch" {
			return Snapshot{}, invalid("ChecksumMismatch", messages.UploadsTheStorageRejectedThePartSHA256Does)
		}
		return Snapshot{}, err
	}
	if reader.left != 0 {
		return Snapshot{}, invalid("IncompleteUpload", messages.UploadsThePartWasOnlyPartiallyUploadedRetrieve)
	}
	if base64.StdEncoding.EncodeToString(reader.hash.Sum(nil)) != spec.Value {
		return Snapshot{}, invalid("ChecksumMismatch", messages.UploadsTheUploadedPartSSHA256ChecksumDoes)
	}
	parts, err = o.serverParts(ctx, c)
	if err != nil {
		return Snapshot{}, err
	}
	o.mu.Lock()
	o.parts = parts
	o.mu.Unlock()
	return o.Snapshot(), nil
}

// partReader streams one declared part without buffering. The native checksum
// protects S3 before acceptance; the local digest independently checks what the
// API supplied. A failed request leaves the multipart alive for reconciliation.
type partReader struct {
	reader io.Reader
	left   int64
	hash   interface {
		Write([]byte) (int, error)
		Sum([]byte) []byte
	}
}

func (r *partReader) Read(p []byte) (int, error) {
	if r.left == 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.left {
		p = p[:r.left]
	}
	n, err := r.reader.Read(p)
	if n > 0 {
		r.left -= int64(n)
		_, _ = r.hash.Write(p[:n])
	}
	if err == io.EOF && r.left != 0 {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}

func SignPart(ctx context.Context, store Store, c Client, id string, n int, request PartRequest) (SignedPart, error) {
	spec, err := partChecksum(request.Checksum)
	if err != nil {
		return SignedPart{}, err
	}
	o, release, err := acquire(ctx, store, c, id)
	if err != nil {
		return SignedPart{}, err
	}
	defer release()
	ctx, cancel := o.actionContext(ctx)
	defer cancel()
	if n < 1 || n > o.count {
		return SignedPart{}, invalid("InvalidPartNumber", messages.HttpInvalidPartNumber)
	}
	if err = canWrite(c, o.bucket, o.key); err != nil {
		return SignedPart{}, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.state != "active" {
		return SignedPart{}, transitionError()
	}
	if old := o.sealed[n]; old != "" && old != spec.Value {
		return SignedPart{}, apperror.New(409, "PartContentConflict", messages.UploadsPartContentConflictParts)
	}
	expires := minTime(time.Now().Add(SignedTTL), o.expires)
	duration := time.Until(expires).Truncate(time.Second)
	if duration < time.Second {
		return SignedPart{}, apperror.New(410, "MultipartExpired", messages.UploadsMultipartExpiredParts)
	}
	u, err := c.PresignMultipartPart(ctx, o.bucket, o.key, o.uploadID, n, spec.Value, duration)
	if err != nil {
		return SignedPart{}, err
	}
	// SDK truncates signed lifetime to seconds. Derive the actual boundary from
	// its signed fields, rather than promising a later local timestamp.
	date, err := time.Parse("20060102T150405Z", u.Query().Get("X-Amz-Date"))
	if err != nil {
		return SignedPart{}, fmt.Errorf("invalid SDK signing date")
	}
	seconds, err := strconv.Atoi(u.Query().Get("X-Amz-Expires"))
	if err != nil {
		return SignedPart{}, fmt.Errorf("invalid SDK signing expiry")
	}
	o.sealed[n] = spec.Value
	return SignedPart{ID: o.id, PartNumber: n, URL: u.String(), Method: http.MethodPut, Headers: map[string]string{"x-amz-checksum-sha256": spec.Value}, ExpiresAt: date.Add(time.Duration(seconds) * time.Second), Size: o.partSize(n), SizeEnforcement: "verified_before_complete", Verification: "S3 ListParts; browser response is not confirmation"}, nil
}

func composite(parts []Part) string {
	ordered := append([]Part{}, parts...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Number < ordered[j].Number })
	h := sha256.New()
	for _, p := range ordered {
		v, _ := base64.StdEncoding.DecodeString(p.Checksum)
		_, _ = h.Write(v)
	}
	return base64.StdEncoding.EncodeToString(h.Sum(nil)) + "-" + partNumber(len(parts))
}

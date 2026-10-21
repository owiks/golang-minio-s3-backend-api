package multipart

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/iam"
	"minio-web/api/src/services/uploads/integrity"
)

type memoryStore struct {
	mu    sync.Mutex
	items map[string]*Operation
}

func (s *memoryStore) Add(o *Operation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = map[string]*Operation{}
	}
	s.items[o.ID()] = o
	return nil
}
func (s *memoryStore) Get(id string) *Operation { s.mu.Lock(); defer s.mu.Unlock(); return s.items[id] }
func (s *memoryStore) Discard(o *Operation) {
	s.mu.Lock()
	if s.items[o.ID()] == o {
		delete(s.items, o.ID())
	}
	s.mu.Unlock()
}

type fixtureClient struct {
	mu                                               sync.Mutex
	owner                                            string
	parts                                            map[int]minio.ObjectPart
	object                                           minio.ObjectInfo
	createCalls, putCalls, completeCalls, abortCalls int
	createErr, partErr, completeErr, abortErr        error
	completeHeaders                                  http.Header
	completeEntered                                  chan struct{}
	completeRelease                                  chan struct{}
	signCalls                                        int
	badParts                                         bool
}

type blockedBody struct {
	entered             chan struct{}
	closed              chan struct{}
	readOnce, closeOnce sync.Once
}

func (b *blockedBody) Read([]byte) (int, error) {
	b.readOnce.Do(func() { close(b.entered) })
	<-b.closed
	return 0, io.ErrClosedPipe
}
func (b *blockedBody) Close() error { b.closeOnce.Do(func() { close(b.closed) }); return nil }

func TestMultipartStopInterruptsBlockedOwnedPartReader(t *testing.T) {
	s := &memoryStore{}
	c := newFixture()
	v := createFixture(t, s, c, 1)
	body := &blockedBody{entered: make(chan struct{}), closed: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := PutPart(context.Background(), s, c, v.ID, 1, 1, digest([]byte("x")), body)
		done <- err
	}()
	<-body.entered
	s.Get(v.ID).Stop()
	select {
	case err := <-done:
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel left part reader blocked")
	}
	if c.abortCalls != 0 {
		t.Fatal("part cancellation aborted resumable upload")
	}
}

type unconfiguredFixture struct{ *fixtureClient }

func (c unconfiguredFixture) ConditionalCompletionConfigured() bool { return false }
func TestMultipartUnverifiedConditionalCompleteCreatesNothing(t *testing.T) {
	c := newFixture()
	s := &memoryStore{}
	_, err := Create(context.Background(), s, unconfiguredFixture{c}, CreateRequest{Bucket: "fixture-bucket", Key: "file.bin", Revision: "absent", Size: 1})
	requireCode(t, err, "ConditionalCompletionUnverified")
	if c.createCalls != 0 || len(s.items) != 0 {
		t.Fatal("unverified conditional support caused side effect")
	}
}

func TestMultipartDefinitiveCreateFailureDoesNotConsumeRegistryCapacity(t *testing.T) {
	c := newFixture()
	c.createErr = minio.ErrorResponse{Code: "AccessDenied", StatusCode: 403}
	s := &memoryStore{}
	for i := 0; i < 20; i++ {
		_, err := Create(context.Background(), s, c, CreateRequest{Bucket: "fixture-bucket", Key: "file.bin", Revision: "absent", Size: 1})
		if minio.ToErrorResponse(err).Code != "AccessDenied" {
			t.Fatal(err)
		}
	}
	if len(s.items) != 0 || c.createCalls != 20 {
		t.Fatal("definitively rejected creates consumed registry slots")
	}
}

func newFixture() *fixtureClient {
	return &fixtureClient{owner: "owner", parts: map[int]minio.ObjectPart{}}
}
func (c *fixtureClient) SessionID() string                     { return c.owner }
func (c *fixtureClient) SessionExpiresAt() time.Time           { return time.Now().Add(time.Hour) }
func (c *fixtureClient) ActorName() string                     { return "fixture" }
func (c *fixtureClient) ActionPolicySnapshot() *iam.Policy     { return nil }
func (c *fixtureClient) ConditionalCompletionConfigured() bool { return true }
func (c *fixtureClient) StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.object.ETag == "" {
		return minio.ObjectInfo{}, minio.ErrorResponse{Code: "NoSuchKey", StatusCode: 404}
	}
	return c.object, nil
}
func (c *fixtureClient) NewMultipartUpload(_ context.Context, _, _ string, opts minio.PutObjectOptions) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.createCalls++
	if c.createErr != nil {
		return "", c.createErr
	}
	if opts.Header().Get("X-Amz-Checksum-Algorithm") != "SHA256" {
		return "", fmt.Errorf("missing checksum initiation")
	}
	return "native-id", nil
}
func (c *fixtureClient) PutObjectPart(_ context.Context, _, _, _ string, n int, r io.Reader, size int64, opts minio.PutObjectPartOptions) (minio.ObjectPart, error) {
	c.mu.Lock()
	c.putCalls++
	err := c.partErr
	c.mu.Unlock()
	if err != nil {
		return minio.ObjectPart{}, err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return minio.ObjectPart{}, err
	}
	if int64(len(data)) != size {
		return minio.ObjectPart{}, io.ErrUnexpectedEOF
	}
	digest := sha256.Sum256(data)
	checksum := base64.StdEncoding.EncodeToString(digest[:])
	if checksum != opts.CustomHeader.Get("X-Amz-Checksum-Sha256") {
		return minio.ObjectPart{}, minio.ErrorResponse{Code: "BadDigest", StatusCode: 400}
	}
	p := minio.ObjectPart{PartNumber: n, Size: size, ETag: fmt.Sprintf("part-%d", n), ChecksumSHA256: checksum}
	c.mu.Lock()
	c.parts[n] = p
	c.mu.Unlock()
	return p, nil
}
func (c *fixtureClient) ListObjectParts(_ context.Context, _, _, _ string, marker, max int) (minio.ListObjectPartsResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := minio.ListObjectPartsResult{Bucket: "fixture-bucket", Key: "file.bin", UploadID: "native-id"}
	if c.badParts {
		v.IsTruncated = true
		return v, nil
	}
	for n := marker + 1; n <= MaxParts && len(v.ObjectParts) < max; n++ {
		if p, ok := c.parts[n]; ok {
			v.ObjectParts = append(v.ObjectParts, p)
		}
	}
	return v, nil
}
func (c *fixtureClient) CompleteMultipartUpload(ctx context.Context, _, _, _ string, parts []minio.CompletePart, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	c.mu.Lock()
	c.completeCalls++
	c.completeHeaders = opts.Header().Clone()
	err := c.completeErr
	entered, release := c.completeEntered, c.completeRelease
	c.mu.Unlock()
	if entered != nil {
		close(entered)
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return minio.UploadInfo{}, ctx.Err()
		}
	}
	if err != nil {
		return minio.UploadInfo{}, err
	}
	return minio.UploadInfo{ETag: "written", VersionID: "v1", ChecksumSHA256: opts.Header().Get("X-Amz-Checksum-Sha256"), ChecksumMode: "COMPOSITE"}, nil
}
func (c *fixtureClient) AbortMultipartUpload(context.Context, string, string, string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.abortCalls++
	return c.abortErr
}
func (c *fixtureClient) PresignMultipartPart(_ context.Context, _, _, _ string, n int, _ string, expires time.Duration) (*url.URL, error) {
	c.mu.Lock()
	c.signCalls++
	c.mu.Unlock()
	return url.Parse(fmt.Sprintf("https://public.example.test/fixture-bucket/file.bin?uploadId=native-id&partNumber=%d&X-Amz-Date=%s&X-Amz-Expires=%d", n, time.Now().UTC().Format("20060102T150405Z"), int(expires.Seconds())))
}

func digest(data []byte) integrity.Spec {
	h := sha256.Sum256(data)
	return integrity.Spec{Algorithm: "SHA256", Encoding: "base64", Value: base64.StdEncoding.EncodeToString(h[:]), Type: "FULL_OBJECT", Scope: "PART"}
}
func createFixture(t *testing.T, s *memoryStore, c *fixtureClient, size int64) Snapshot {
	t.Helper()
	v, err := Create(context.Background(), s, c, CreateRequest{Bucket: "fixture-bucket", Key: "file.bin", Revision: "absent", Size: size})
	if err != nil || v.State != "active" {
		t.Fatalf("create %#v %v", v, err)
	}
	return v
}
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil || apperror.Public(err).Code != code {
		t.Fatalf("want %s got %v", code, err)
	}
}

func TestMultipartResumeSealedPartAndIdempotentComplete(t *testing.T) {
	s := &memoryStore{}
	c := newFixture()
	data := []byte("resumable payload")
	v := createFixture(t, s, c, int64(len(data)))
	// A short HTTP body must not abort the whole S3 multipart upload.
	_, err := PutPart(context.Background(), s, c, v.ID, 1, int64(len(data)), digest(data), bytes.NewReader(data[:4]))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	if c.abortCalls != 0 {
		t.Fatal("failed part aborted upload")
	}
	state, err := Inspect(context.Background(), s, c, v.ID)
	if err != nil || len(state.Parts) != 0 {
		t.Fatal(state, err)
	}
	state, err = PutPart(context.Background(), s, c, v.ID, 1, int64(len(data)), digest(data), bytes.NewReader(data))
	if err != nil || len(state.Parts) != 1 {
		t.Fatal(state, err)
	}
	_, err = PutPart(context.Background(), s, c, v.ID, 1, int64(len(data)), digest(data), bytes.NewReader(data))
	if err != nil || c.putCalls != 2 {
		t.Fatal("duplicate repeated accepted part", err, c.putCalls)
	}
	_, err = PutPart(context.Background(), s, c, v.ID, 1, int64(len(data)), digest([]byte("other")), bytes.NewReader(data))
	requireCode(t, err, "PartContentConflict")
	complete, err := Complete(context.Background(), s, c, v.ID)
	if err != nil || complete.State != "completed" || complete.Receipt == nil {
		t.Fatal(complete, err)
	}
	if c.completeHeaders.Get("If-None-Match") != "*" || c.completeHeaders.Get("X-Amz-Checksum-Sha256") == "" {
		t.Fatal("conditional complete lost", c.completeHeaders)
	}
	complete.Receipt.Checksum.Computed.Value = "mutated"
	complete.Parts[0].Checksum = "mutated"
	again, err := Complete(context.Background(), s, c, v.ID)
	if err != nil || c.completeCalls != 1 || again.Receipt.Checksum.Computed.Value == "mutated" || again.Parts[0].Checksum == "mutated" {
		t.Fatal("receipt escaped or repeat complete", again, err, c.completeCalls)
	}
}

func TestMultipartForeignSessionAndBoundPlan(t *testing.T) {
	s := &memoryStore{}
	c := newFixture()
	v := createFixture(t, s, c, 5)
	foreign := newFixture()
	foreign.owner = "other"
	_, err := Inspect(context.Background(), s, foreign, v.ID)
	requireCode(t, err, "MultipartNotFound")
	_, err = PutPart(context.Background(), s, foreign, v.ID, 1, 5, digest([]byte("12345")), bytes.NewReader([]byte("12345")))
	requireCode(t, err, "MultipartNotFound")
	_, err = SignPart(context.Background(), s, foreign, v.ID, 1, PartRequest{Checksum: digest([]byte("12345"))})
	requireCode(t, err, "MultipartNotFound")
	_, err = Complete(context.Background(), s, foreign, v.ID)
	requireCode(t, err, "MultipartNotFound")
	_, err = Abort(context.Background(), s, foreign, v.ID)
	requireCode(t, err, "MultipartNotFound")
	for _, n := range []int{0, 2, 321} {
		_, err = PutPart(context.Background(), s, c, v.ID, n, 5, digest([]byte("12345")), bytes.NewReader(nil))
		requireCode(t, err, "InvalidPartSize")
	}
	_, err = PutPart(context.Background(), s, c, v.ID, 1, 6, digest([]byte("12345")), bytes.NewReader(nil))
	requireCode(t, err, "InvalidPartSize")
	if foreign.putCalls+foreign.signCalls+foreign.completeCalls+foreign.abortCalls+c.putCalls != 0 {
		t.Fatal("invalid call reached modifying upstream")
	}
}

func TestMultipartUnknownOutcomesNeverRepeatCommands(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		s := &memoryStore{}
		c := newFixture()
		c.createErr = io.ErrUnexpectedEOF
		v, err := Create(context.Background(), s, c, CreateRequest{Bucket: "fixture-bucket", Key: "file.bin", Revision: "absent", Size: 1})
		if err != nil || v.State != "create_uncertain" || v.ID == "" {
			t.Fatal(v, err)
		}
		_, err = Inspect(context.Background(), s, c, v.ID)
		if err != nil || c.createCalls != 1 {
			t.Fatal(err, c.createCalls)
		}
	})
	t.Run("complete", func(t *testing.T) {
		s := &memoryStore{}
		c := newFixture()
		data := []byte("a")
		v := createFixture(t, s, c, 1)
		_, err := PutPart(context.Background(), s, c, v.ID, 1, 1, digest(data), bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		c.completeErr = io.ErrUnexpectedEOF
		v, err = Complete(context.Background(), s, c, v.ID)
		if err != nil || v.State != "complete_uncertain" {
			t.Fatal(v, err)
		}
		v, err = Complete(context.Background(), s, c, v.ID)
		if err != nil || v.State != "complete_uncertain" || c.completeCalls != 1 {
			t.Fatal(v, err, c.completeCalls)
		}
		// Presence alone, or marker with no checksum, is insufficient evidence.
		c.object = minio.ObjectInfo{ETag: "other", Size: 1, UserMetadata: map[string]string{markerKey: v.ID}}
		v, err = Inspect(context.Background(), s, c, v.ID)
		if err != nil || v.State != "complete_uncertain" {
			t.Fatal(v, err)
		}
		c.object.ChecksumSHA256 = composite(s.Get(v.ID).parts)
		v, err = Inspect(context.Background(), s, c, v.ID)
		if err != nil || v.State != "completed" || c.completeCalls != 1 {
			t.Fatal(v, err)
		}
	})
}

func TestMultipartConflictsAreNotRetriedAndWrongPartsBlockComplete(t *testing.T) {
	s := &memoryStore{}
	c := newFixture()
	data := []byte("a")
	v := createFixture(t, s, c, 1)
	_, err := PutPart(context.Background(), s, c, v.ID, 1, 1, digest(data), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	c.completeErr = minio.ErrorResponse{Code: "PreconditionFailed", StatusCode: 412}
	_, err = Complete(context.Background(), s, c, v.ID)
	requireCode(t, err, "DestinationChanged")
	if c.completeCalls != 1 || c.completeHeaders.Get("If-None-Match") != "*" {
		t.Fatal("unsafe repeat")
	}
	c.badParts = true
	_, err = Complete(context.Background(), s, c, v.ID)
	requireCode(t, err, "InvalidMultipartParts")
	if c.completeCalls != 1 {
		t.Fatal("bad parts reached complete")
	}
}

func TestMultipartCancelWaitAndCleanupBound(t *testing.T) {
	s := &memoryStore{}
	c := newFixture()
	data := []byte("a")
	v := createFixture(t, s, c, 1)
	_, err := PutPart(context.Background(), s, c, v.ID, 1, 1, digest(data), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	c.completeEntered = make(chan struct{})
	c.completeRelease = make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); _, _ = Complete(context.Background(), s, c, v.ID) }()
	<-c.completeEntered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = Abort(ctx, s, c, v.ID)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	close(c.completeRelease)
	<-done
	if c.abortCalls != 0 {
		t.Fatal("abort raced through complete")
	}
	o := s.Get(v.ID)
	if !o.Cleanup(context.Background(), time.Now()) {
		t.Fatal("completed retained cleanup")
	}
	_, err = SignPart(context.Background(), s, c, v.ID, 1, PartRequest{Checksum: digest(data)})
	requireCode(t, err, "MultipartExpired")
	// Explicit cleanup retries are bounded and do not claim network failures as success.
	s2 := &memoryStore{}
	c2 := newFixture()
	v2 := createFixture(t, s2, c2, 1)
	o2 := s2.Get(v2.ID)
	c2.abortErr = io.ErrUnexpectedEOF
	now := time.Now()
	for i := 0; i < 5; i++ {
		if o2.Cleanup(context.Background(), now.Add(time.Duration(i)*time.Minute)) {
			t.Fatal("failed abort claimed cleanup")
		}
	}
	if c2.abortCalls != 3 {
		t.Fatal("unbounded cleanup", c2.abortCalls)
	}
}

func TestMultipartSignedPartSealsContentAndPreservesSizeBoundary(t *testing.T) {
	s := &memoryStore{}
	c := newFixture()
	v := createFixture(t, s, c, 17)
	signed, err := SignPart(context.Background(), s, c, v.ID, 1, PartRequest{Checksum: digest([]byte("payload"))})
	if err != nil {
		t.Fatal(err)
	}
	if signed.Method != "PUT" || signed.Size != 17 || signed.Headers["x-amz-checksum-sha256"] == "" || signed.SizeEnforcement != "verified_before_complete" || signed.Headers["Content-Length"] != "" {
		t.Fatal(signed)
	}
	_, err = SignPart(context.Background(), s, c, v.ID, 1, PartRequest{Checksum: digest([]byte("changed"))})
	requireCode(t, err, "PartContentConflict")
	if c.signCalls != 1 {
		t.Fatal("conflict issued new signature")
	}
}

func TestMultipartWrongBodyChecksumCanResumeSameExpectedPart(t *testing.T) {
	s := &memoryStore{}
	c := newFixture()
	expected := []byte("good")
	v := createFixture(t, s, c, 4)
	_, err := PutPart(context.Background(), s, c, v.ID, 1, 4, digest(expected), bytes.NewReader([]byte("evil")))
	requireCode(t, err, "ChecksumMismatch")
	if c.abortCalls != 0 || len(c.parts) != 0 {
		t.Fatal("bad body committed or aborted upload")
	}
	_, err = PutPart(context.Background(), s, c, v.ID, 1, 4, digest(expected), bytes.NewReader(expected))
	if err != nil {
		t.Fatal(err)
	}
}

// The runtime can detach an exhausted record while another cleanup sweep still
// owns its action lock. An in-flight attempt must retain a stable client and
// must not publish exhaustion until the upstream call has actually returned.
type gatedCleanupClient struct {
	*fixtureClient
	cleanupMu sync.Mutex
	entered   chan struct{}
	release   chan struct{}
	attempts  int
	observed  int
}

func (c *gatedCleanupClient) AbortMultipartUpload(ctx context.Context, _, _, _ string) error {
	c.cleanupMu.Lock()
	c.attempts++
	attempt := c.attempts
	c.cleanupMu.Unlock()
	if attempt == 3 {
		close(c.entered)
		select {
		case <-c.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return io.ErrUnexpectedEOF
}

func (c *gatedCleanupClient) ObserveMultipart(_ context.Context, operation, _, _ string, err error) {
	if operation == "abort" && err != nil {
		c.cleanupMu.Lock()
		c.observed++
		c.cleanupMu.Unlock()
	}
}

func TestMultipartReleaseCleanupRetainsClientForInFlightFinalAttempt(t *testing.T) {
	c := &gatedCleanupClient{fixtureClient: newFixture(), entered: make(chan struct{}), release: make(chan struct{})}
	s := &memoryStore{}
	v, err := Create(context.Background(), s, c, CreateRequest{Bucket: "fixture-bucket", Key: "file.bin", Revision: "absent", Size: 1})
	if err != nil {
		t.Fatal(err)
	}
	o := s.Get(v.ID)
	now := time.Now()
	for i := range 2 {
		if o.Cleanup(context.Background(), now.Add(time.Duration(i)*time.Minute)) {
			t.Fatal("failed cleanup claimed upstream removal")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var releaseOnce sync.Once
	openGate := func() { releaseOnce.Do(func() { close(c.release) }) }
	defer openGate()
	done := make(chan bool, 1)
	go func() { done <- o.Cleanup(ctx, now.Add(2*time.Minute)) }()
	select {
	case <-c.entered:
	case <-ctx.Done():
		t.Fatal("final cleanup did not reach upstream")
	}
	if o.CleanupExhausted() {
		t.Fatal("cleanup was reported exhausted before the final abort returned")
	}
	// Force the same detachment that a concurrent sweep can perform. The final
	// abort and its diagnostic observation must keep their own client reference.
	o.ReleaseCleanup()
	openGate()
	select {
	case removed := <-done:
		if removed {
			t.Fatal("lost abort response became confirmed cleanup")
		}
	case <-ctx.Done():
		t.Fatal("detached in-flight cleanup remained blocked")
	}
	c.cleanupMu.Lock()
	defer c.cleanupMu.Unlock()
	if c.attempts != 3 || c.observed != 3 || !o.CleanupExhausted() {
		t.Fatal("cleanup client was lost, retry limit changed, or final result was not published", c.attempts, c.observed)
	}
}

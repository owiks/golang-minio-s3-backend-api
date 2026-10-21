package objects

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/result"
	"minio-web/api/src/services/telemetry"
)

type checksumRunWire func(*http.Request) (*http.Response, error)

func (f checksumRunWire) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type checksumRunBody struct {
	io.Reader
	onClose func()
	once    sync.Once
}

func (b *checksumRunBody) Close() error { b.once.Do(b.onClose); return nil }

type checksumRunSource struct {
	client                *minio.Client
	gets, heads, releases atomic.Int32
	mu                    sync.Mutex
	paths                 []string
	readContext           context.Context
	released              chan struct{}
}

func newChecksumRunSource(t *testing.T, body []byte, reader func(context.Context) io.Reader, finalHead func(context.Context) error) *checksumRunSource {
	t.Helper()
	fixture := &checksumRunSource{released: make(chan struct{})}
	sum := sha256.Sum256(body)
	wire := checksumRunWire(func(r *http.Request) (*http.Response, error) {
		fixture.mu.Lock()
		fixture.paths = append(fixture.paths, r.Method+" "+r.URL.Path)
		if r.Method == "GET" {
			fixture.readContext = r.Context()
		}
		fixture.mu.Unlock()
		data := io.Reader(strings.NewReader(""))
		if r.Method == "HEAD" {
			if fixture.heads.Add(1) > 1 && finalHead != nil {
				if err := finalHead(r.Context()); err != nil {
					return nil, err
				}
			}
		} else if r.Method == "GET" {
			fixture.gets.Add(1)
			data = bytes.NewReader(body)
			if reader != nil {
				data = reader(r.Context())
			}
		} else {
			t.Errorf("unexpected checksum request: %s", r.Method)
		}
		headers := http.Header{}
		headers.Set("Content-Length", strconv.Itoa(len(body)))
		headers.Set("Last-Modified", time.Unix(1700000000, 0).UTC().Format(http.TimeFormat))
		headers.Set("ETag", `"etag"`)
		headers.Set("X-Amz-Checksum-Sha256", base64.StdEncoding.EncodeToString(sum[:]))
		headers.Set("X-Amz-Checksum-Type", "FULL_OBJECT")
		responseBody := io.ReadCloser(io.NopCloser(data))
		if r.Method == "GET" {
			responseBody = &checksumRunBody{Reader: data, onClose: func() { fixture.releases.Add(1); close(fixture.released) }}
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: headers, Body: responseBody, ContentLength: int64(len(body)), Request: r}, nil
	})
	client, err := minio.New("fixture.invalid", &minio.Options{Region: "us-east-1", BucketLookup: minio.BucketLookupPath, MaxRetries: 1, Transport: wire})
	if err != nil {
		t.Fatal(err)
	}
	fixture.client = client
	return fixture
}

func (s *checksumRunSource) getContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readContext
}

func prepareChecksumRun(t *testing.T, ctx context.Context, source *checksumRunSource, expected string) *ChecksumRun {
	t.Helper()
	run, err := PrepareChecksum(ctx, source.client, "bucket", "folder/file.bin", expected, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(run.Close)
	return run
}

func waitChecksumSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("checksum operation did not reach the expected stage")
	}
}

func waitChecksumError(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("checksum operation did not stop")
		return nil
	}
}

func TestChecksumRunOwnsSummaryAndPreparedScope(t *testing.T) {
	body := bytes.Repeat([]byte("integrity"), 32*1024)
	source := newChecksumRunSource(t, body, nil, nil)
	sum := sha256.Sum256(body)
	expected := strings.ToUpper(hex.EncodeToString(sum[:]))
	run := prepareChecksumRun(t, context.Background(), source, expected)
	view := run.Summary()
	view.Size, view.Version, view.ETag = -1, "other", "other"
	view.Checksums[0].Algorithm, view.Checksums[0].Value = "MD5", "wrong"
	expected = strings.Repeat("0", 64)
	var value *ChecksumResult
	err := run.Stream(context.Background(), func(event result.StreamEvent[ChecksumResult]) bool { value = event.Result; return true })
	if err != nil || value == nil || value.Status != "matched" || value.Bytes != int64(len(body)) || len(value.Comparisons) != 2 || value.Comparisons[0].Expected == expected {
		t.Fatal("prepared inputs or references were changed from outside", value, err)
	}
	if actual := run.Summary(); actual.Size != int64(len(body)) || actual.Checksums[0].Algorithm != "SHA256" || actual.Checksums[0].Value == "wrong" {
		t.Fatal("summary aliases the operation", actual)
	}
	waitChecksumSignal(t, source.released)
	if source.gets.Load() != 1 || source.heads.Load() != 2 || source.releases.Load() != 1 {
		t.Fatal("unexpected S3 lifecycle", source.gets.Load(), source.heads.Load(), source.releases.Load())
	}
	for _, path := range source.paths {
		if path != "GET /bucket/folder/file.bin" && path != "HEAD /bucket/folder/file.bin" {
			t.Fatal("scope changed after preparation", path)
		}
	}
	for range 3 {
		run.Close()
	}
	if source.releases.Load() != 1 {
		t.Fatal("late Close repeated resource release")
	}
}

func TestChecksumRunRejectsRepeatAndSummaryOnlyConsumers(t *testing.T) {
	source := newChecksumRunSource(t, []byte("body"), nil, nil)
	run := prepareChecksumRun(t, context.Background(), source, "")
	if err := run.Stream(context.Background(), func(result.StreamEvent[ChecksumResult]) bool { return true }); err != nil {
		t.Fatal(err)
	}
	assertRejected := func(run *ChecksumRun, code string) {
		t.Helper()
		calls := 0
		err := run.Stream(context.Background(), func(result.StreamEvent[ChecksumResult]) bool { calls++; return true })
		if err == nil || apperror.Public(err).Code != code || calls != 0 {
			t.Fatal("rejected consumer emitted or entered S3", err, calls)
		}
	}
	assertRejected(run, "ChecksumStarted")
	metadata, err := PrepareChecksum(context.Background(), source.client, "bucket", "folder/file.bin", "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer metadata.Close()
	assertRejected(metadata, "ChecksumNotPrepared")
	metadata.Close()
	assertRejected(metadata, "ChecksumClosed")
	zero := &ChecksumRun{}
	assertRejected(zero, "ChecksumNotPrepared")
	zero.Close()
	assertRejected(zero, "ChecksumClosed")
	if source.gets.Load() != 1 || source.heads.Load() != 3 {
		t.Fatal("rejected consumer caused another upstream request")
	}
}

func TestChecksumRunEarlyCloseReleasesAndPreventsStreaming(t *testing.T) {
	source := newChecksumRunSource(t, bytes.Repeat([]byte("x"), 256*1024), nil, nil)
	run := prepareChecksumRun(t, context.Background(), source, "")
	var work sync.WaitGroup
	for range 16 {
		work.Go(run.Close)
	}
	work.Wait()
	waitChecksumSignal(t, source.released)
	if source.getContext().Err() != context.Canceled || source.releases.Load() != 1 {
		t.Fatal("early Close did not release the prepared reader")
	}
	err := run.Stream(context.Background(), func(result.StreamEvent[ChecksumResult]) bool { t.Error("closed run emitted"); return true })
	if err == nil || apperror.Public(err).Code != "ChecksumClosed" || source.heads.Load() != 1 || source.gets.Load() != 1 {
		t.Fatal("closed run was restarted", err)
	}
}

type checksumStalledReader struct {
	ctx     context.Context
	first   []byte
	entered chan struct{}
	once    sync.Once
}

func (r *checksumStalledReader) Read(p []byte) (int, error) {
	if len(r.first) > 0 {
		n := copy(p, r.first)
		r.first = r.first[n:]
		return n, nil
	}
	r.once.Do(func() { close(r.entered) })
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}

func TestChecksumRunCancellationReleasesActiveIO(t *testing.T) {
	for _, stage := range []string{"read", "final head"} {
		for _, how := range []string{"Close", "stream context", "parent context"} {
			t.Run(stage+"/"+how, func(t *testing.T) {
				parent, cancelParent := context.WithCancel(context.Background())
				defer cancelParent()
				streamCtx, cancelStream := context.WithCancel(context.Background())
				defer cancelStream()
				ctx, trace := telemetry.Start(streamCtx, "checksum")
				entered := make(chan struct{})
				body := bytes.Repeat([]byte("x"), 256*1024)
				var reader func(context.Context) io.Reader
				var head func(context.Context) error
				if stage == "read" {
					reader = func(ctx context.Context) io.Reader {
						return &checksumStalledReader{ctx: ctx, first: bytes.Clone(body[:128*1024]), entered: entered}
					}
				} else {
					head = func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }
				}
				source := newChecksumRunSource(t, body, reader, head)
				run := prepareChecksumRun(t, parent, source, "")
				done := make(chan error, 1)
				var results atomic.Int32
				go func() {
					done <- run.Stream(ctx, func(event result.StreamEvent[ChecksumResult]) bool {
						if event.Result != nil {
							results.Add(1)
						}
						return true
					})
				}()
				waitChecksumSignal(t, entered)
				switch how {
				case "Close":
					closed := make(chan struct{})
					go func() { run.Close(); close(closed) }()
					waitChecksumSignal(t, closed)
				case "stream context":
					cancelStream()
				case "parent context":
					cancelParent()
				}
				err := waitChecksumError(t, done)
				waitChecksumSignal(t, source.released)
				if !errors.Is(err, context.Canceled) || results.Load() != 0 || source.releases.Load() != 1 {
					t.Fatal("cancelled checksum retained resources or reported success", err, results.Load(), source.releases.Load())
				}
				if how != "parent context" && parent.Err() != nil {
					t.Fatal("operation cancellation escaped into its caller")
				}
				if trace.ResultName(ctx, 200) != "cancelled" {
					t.Fatal("cancellation became success", trace.Result, trace.Code)
				}
			})
		}
	}
}

func TestChecksumRunConcurrentConsumersDoNotShareTheReader(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	source := newChecksumRunSource(t, bytes.Repeat([]byte("x"), 256*1024), nil, nil)
	run := prepareChecksumRun(t, context.Background(), source, "")
	done := make(chan error, 1)
	go func() {
		done <- run.Stream(context.Background(), func(result.StreamEvent[ChecksumResult]) bool { close(entered); <-release; return false })
	}()
	waitChecksumSignal(t, entered)
	var work sync.WaitGroup
	for range 16 {
		work.Go(func() {
			err := run.Stream(context.Background(), func(result.StreamEvent[ChecksumResult]) bool { t.Error("duplicate consumer emitted"); return true })
			if err == nil || apperror.Public(err).Code != "ChecksumStarted" {
				t.Error("duplicate consumer was not rejected", err)
			}
		})
	}
	work.Wait()
	run.Close()
	select {
	case <-done:
		t.Fatal("stream released its callback before it returned")
	default:
	}
	once.Do(func() { close(release) })
	err := waitChecksumError(t, done)
	waitChecksumSignal(t, source.released)
	if !errors.Is(err, context.Canceled) || source.gets.Load() != 1 || source.heads.Load() != 1 || source.releases.Load() != 1 {
		t.Fatal("concurrent consumers changed the reader lifecycle", err)
	}
}

func TestChecksumRunSummaryCanBeEditedDuringStreaming(t *testing.T) {
	body := bytes.Repeat([]byte("integrity"), 128*1024)
	source := newChecksumRunSource(t, body, nil, nil)
	run := prepareChecksumRun(t, context.Background(), source, "")
	var work sync.WaitGroup
	for range 16 {
		work.Go(func() {
			for range 100 {
				view := run.Summary()
				view.Checksums[0].Value = "caller-owned"
				view.Checksums = append(view.Checksums, StoredChecksum{Algorithm: "MD5", Value: "wrong", Type: "FULL_OBJECT"})
			}
		})
	}
	var value *ChecksumResult
	err := run.Stream(context.Background(), func(event result.StreamEvent[ChecksumResult]) bool {
		if event.Result != nil {
			value = event.Result
		}
		return true
	})
	work.Wait()
	if err != nil || value == nil || value.Status != "matched" || len(value.Comparisons) != 1 {
		t.Fatal("concurrent summary edits reached the checksum references", value, err)
	}
}

func TestChecksumRunCallbackFailureAndPanicReleaseResources(t *testing.T) {
	for _, how := range []string{"reject", "panic", "close in progress", "close after result"} {
		t.Run(how, func(t *testing.T) {
			source := newChecksumRunSource(t, []byte("body"), nil, nil)
			run := prepareChecksumRun(t, context.Background(), source, "")
			ctx, trace := telemetry.Start(context.Background(), "checksum")
			var streamErr error
			var value *ChecksumResult
			func() {
				defer func() {
					p := recover()
					if (p != nil) != (how == "panic") || p != nil && p != "checksum callback failed" {
						t.Error("unexpected callback panic", p)
					}
				}()
				streamErr = run.Stream(ctx, func(event result.StreamEvent[ChecksumResult]) bool {
					if how == "panic" {
						panic("checksum callback failed")
					}
					if how == "reject" {
						return false
					}
					if how == "close in progress" || event.Result != nil {
						run.Close()
					}
					if event.Result != nil {
						value = event.Result
					}
					return true
				})
			}()
			waitChecksumSignal(t, source.released)
			if source.releases.Load() != 1 || source.getContext().Err() != context.Canceled {
				t.Fatal("callback retained its S3 reader")
			}
			if how == "close after result" {
				if streamErr != nil || value == nil || value.Status != "matched" {
					t.Fatal("late close changed an accepted result", value, streamErr)
				}
			} else if how != "panic" && !errors.Is(streamErr, context.Canceled) {
				t.Fatal("cancelled callback reported success", streamErr)
			}
			if how == "reject" && trace.Code != "StreamWriteFailed" {
				t.Fatal("write failure telemetry changed", trace.Code)
			}
		})
	}
}

func TestChecksumRunCancelledBeforeStreamingDoesNotPublishAResult(t *testing.T) {
	for _, how := range []string{"parent", "stream"} {
		t.Run(how, func(t *testing.T) {
			parent, cancelParent := context.WithCancel(context.Background())
			defer cancelParent()
			ctx, cancelStream := context.WithCancel(context.Background())
			defer cancelStream()
			source := newChecksumRunSource(t, []byte("body"), nil, nil)
			run := prepareChecksumRun(t, parent, source, "")
			if how == "parent" {
				cancelParent()
			} else {
				cancelStream()
			}
			emitted := 0
			err := run.Stream(ctx, func(result.StreamEvent[ChecksumResult]) bool { emitted++; return true })
			waitChecksumSignal(t, source.released)
			if !errors.Is(err, context.Canceled) || emitted != 0 || source.heads.Load() != 1 || source.gets.Load() != 1 {
				t.Fatal("cancelled preparation started work or emitted success", err, emitted)
			}
		})
	}
}

type checksumReadFailure struct{}

func (checksumReadFailure) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestChecksumRunPreparationFailureReleasesTheOwnedReader(t *testing.T) {
	source := newChecksumRunSource(t, []byte("body"), func(context.Context) io.Reader { return checksumReadFailure{} }, nil)
	run, err := PrepareChecksum(context.Background(), source.client, "bucket", "folder/file.bin", "", true)
	if err == nil || run != nil {
		t.Fatal("incomplete preflight unexpectedly succeeded", run, err)
	}
	waitChecksumSignal(t, source.released)
	if source.getContext().Err() != context.Canceled || source.releases.Load() != 1 {
		t.Fatal("preparation failure retained the S3 context or body")
	}
}

func TestChecksumRunCloseRacesWithFirstConsumer(t *testing.T) {
	for range 40 {
		source := newChecksumRunSource(t, []byte("body"), nil, nil)
		run := prepareChecksumRun(t, context.Background(), source, "")
		start := make(chan struct{})
		var results atomic.Int32
		var work sync.WaitGroup
		for range 8 {
			work.Go(func() { <-start; run.Close() })
			work.Go(func() {
				<-start
				err := run.Stream(context.Background(), func(event result.StreamEvent[ChecksumResult]) bool {
					if event.Result != nil {
						results.Add(1)
					}
					return true
				})
				if err != nil && !errors.Is(err, context.Canceled) {
					code := apperror.Public(err).Code
					if code != "ChecksumStarted" && code != "ChecksumClosed" {
						t.Error("unexpected lifecycle error", err)
					}
				}
			})
		}
		close(start)
		work.Wait()
		waitChecksumSignal(t, source.released)
		if results.Load() > 1 || source.gets.Load() != 1 || source.heads.Load() > 2 || source.releases.Load() != 1 {
			t.Fatal("racing consumers duplicated or retained work", results.Load(), source.heads.Load(), source.gets.Load())
		}
	}
}

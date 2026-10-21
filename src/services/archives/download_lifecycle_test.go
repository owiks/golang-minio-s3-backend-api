package archives

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	archiveplan "minio-web/api/src/services/archives/plan"
	objectstate "minio-web/api/src/services/objects/state"
	"minio-web/api/src/services/telemetry"
)

type downloadSource struct {
	read  func(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
	calls atomic.Int32
}

func (*downloadSource) SessionID() string { return "owner" }
func (s *downloadSource) GetObject(ctx context.Context, bucket, key string, opts minio.GetObjectOptions) (*minio.Object, error) {
	s.calls.Add(1)
	return s.read(ctx, bucket, key, opts)
}

func startTestDownload(t *testing.T, parent context.Context, source *downloadSource, items []archiveplan.ArchiveItem) (*Download, *archiveplan.ArchivePlan, *telemetry.Trace) {
	t.Helper()
	ctx, trace := telemetry.Start(parent, "archive download")
	plan := archiveplan.New(archiveplan.Config{SessionID: "owner", Bucket: "bucket", Name: "test.zip", Created: time.Now(), Items: items})
	download, err := StartDownload(ctx, &archiveLookup{plan: plan}, source, "ticket-12345678")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(download.Close)
	return download, plan, trace
}

func pendingArchiveItem() archiveplan.ArchiveItem {
	return archiveplan.ArchiveItem{Key: "file", Path: "file", State: "pending"}
}

func assertCancelledDownload(t *testing.T, download *Download, plan *archiveplan.ArchivePlan, trace *telemetry.Trace) {
	t.Helper()
	snapshot := plan.Snapshot("ticket-12345678")
	if !snapshot.Started || !snapshot.Finished || !snapshot.Cancelled || snapshot.Items[0].State != "cancelled" {
		t.Fatal("download did not reach a cancelled terminal state", snapshot)
	}
	if !errors.Is(download.Context().Err(), context.Canceled) || trace.ResultName(download.Context(), 200) != "cancelled" || trace.Code != "ArchiveCancelled" {
		t.Fatal("cancellation context or telemetry lost", download.Context().Err(), trace.Result, trace.Code)
	}
}

func TestDownloadCloseBeforeStreamFinishesAndAllowsForget(t *testing.T) {
	source := &downloadSource{read: func(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error) {
		t.Error("closed download read an object")
		return nil, context.Canceled
	}}
	download, plan, trace := startTestDownload(t, context.Background(), source, []archiveplan.ArchiveItem{pendingArchiveItem()})
	var calls sync.WaitGroup
	for range 16 {
		calls.Go(download.Close)
	}
	calls.Wait()
	assertCancelledDownload(t, download, plan, trace)
	var output bytes.Buffer
	if err := download.StreamTo(&output, testManifest); !errors.Is(err, context.Canceled) || output.Len() != 0 || source.calls.Load() != 0 {
		t.Fatal("closed download started streaming", err, output.Len(), source.calls.Load())
	}
	store := &archiveForgetter{archiveLookup: &archiveLookup{plan: plan}}
	if _, err := ForgetArchive(store, "owner", "ticket-12345678"); err != nil || store.forgotten != plan {
		t.Fatal("early close retained an ArchiveRunning plan", err)
	}
	if _, err := StartDownload(context.Background(), &archiveLookup{plan: plan}, source, "ticket-12345678"); apperror.Public(err).Code != "ArchiveStarted" {
		t.Fatal("closed plan was restarted", err)
	}
}

func awaitDownload(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("download did not stop")
		return nil
	}
}

func TestDownloadRejectsConcurrentStreamsAndClosesActiveRead(t *testing.T) {
	entered := make(chan struct{})
	source := &downloadSource{read: func(ctx context.Context, _, _ string, _ minio.GetObjectOptions) (*minio.Object, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	download, plan, trace := startTestDownload(t, context.Background(), source, []archiveplan.ArchiveItem{pendingArchiveItem()})
	done := make(chan error, 1)
	go func() { done <- download.StreamTo(io.Discard, testManifest) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("read did not start")
	}
	var attempts sync.WaitGroup
	for range 16 {
		attempts.Go(func() {
			if err := download.StreamTo(io.Discard, testManifest); apperror.Public(err).Code != "ArchiveStarted" {
				t.Error("duplicate consumer was not rejected", err)
			}
		})
	}
	attempts.Wait()
	download.Close()
	if err := awaitDownload(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal("read cancellation lost", err)
	}
	assertCancelledDownload(t, download, plan, trace)
	if source.calls.Load() != 1 {
		t.Fatal("duplicate stream opened another object", source.calls.Load())
	}
}

func TestDownloadParentCancellationFinishesPlan(t *testing.T) {
	for _, beforeStream := range []bool{true, false} {
		t.Run(map[bool]string{true: "before stream", false: "during read"}[beforeStream], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			source := &downloadSource{read: func(ctx context.Context, _, _ string, _ minio.GetObjectOptions) (*minio.Object, error) {
				close(entered)
				<-ctx.Done()
				return nil, ctx.Err()
			}}
			download, plan, trace := startTestDownload(t, ctx, source, []archiveplan.ArchiveItem{pendingArchiveItem()})
			if beforeStream {
				cancel()
			}
			done := make(chan error, 1)
			go func() { done <- download.StreamTo(io.Discard, testManifest) }()
			if !beforeStream {
				select {
				case <-entered:
				case <-time.After(3 * time.Second):
					t.Fatal("read did not start")
				}
				cancel()
			}
			if err := awaitDownload(t, done); !errors.Is(err, context.Canceled) {
				t.Fatal("parent cancellation lost", err)
			}
			assertCancelledDownload(t, download, plan, trace)
		})
	}
}

func downloadObjectFixture(t *testing.T, body string) (*downloadSource, archiveplan.ArchiveItem) {
	t.Helper()
	modified := time.Unix(1700000000, 0).UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"etag"`)
		w.Header().Set("Last-Modified", modified.Format(http.TimeFormat))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, body)
		}
	}))
	t.Cleanup(server.Close)
	client, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	source := &downloadSource{read: client.GetObject}
	return source, archiveplan.ArchiveItem{Key: "file", Path: "file", Size: int64(len(body)), State: "pending", Info: objectstate.DeletionSnapshot{ETag: "etag", Size: int64(len(body)), Modified: modified}}
}

type blockedArchiveTarget struct {
	entered, release chan struct{}
	once             sync.Once
}

func (w *blockedArchiveTarget) Write([]byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return 0, io.ErrClosedPipe
}

func TestDownloadCloseDoesNotFinalizeWhileStreamOwnsWriter(t *testing.T) {
	source, item := downloadObjectFixture(t, strings.Repeat("x", 128*1024))
	download, plan, trace := startTestDownload(t, context.Background(), source, []archiveplan.ArchiveItem{item})
	target := &blockedArchiveTarget{entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	defer release.Do(func() { close(target.release) })
	done := make(chan error, 1)
	go func() { done <- download.StreamTo(target, testManifest) }()
	select {
	case <-target.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("target write did not start")
	}
	download.Close()
	if snapshot := plan.Snapshot(""); snapshot.Finished || !snapshot.Cancelled {
		t.Fatal("active stream was finalized before it released the writer", snapshot)
	}
	if err := plan.PrepareForget(); apperror.Public(err).Code != "ArchiveRunning" {
		t.Fatal("active stream can be forgotten", err)
	}
	release.Do(func() { close(target.release) })
	if err := awaitDownload(t, done); err == nil {
		t.Fatal("interrupted ZIP reported success")
	}
	assertCancelledDownload(t, download, plan, trace)
}

type failedArchiveTarget struct{}

func (failedArchiveTarget) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestDownloadClosePreservesCompletedOutcome(t *testing.T) {
	for _, kind := range []string{"success", "partial", "write failure"} {
		t.Run(kind, func(t *testing.T) {
			source, item := downloadObjectFixture(t, "hello")
			if kind == "partial" {
				source.read = func(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error) {
					return nil, minio.ErrorResponse{Code: "NoSuchKey", StatusCode: 404}
				}
			}
			download, plan, trace := startTestDownload(t, context.Background(), source, []archiveplan.ArchiveItem{item})
			var output bytes.Buffer
			var target io.Writer = &output
			if kind == "write failure" {
				target = failedArchiveTarget{}
			}
			err := download.StreamTo(target, testManifest)
			wantState, wantResult, wantCode := "done", "success", ""
			if kind == "partial" {
				wantState, wantResult, wantCode = "error", "partial", "ArchivePartial"
			} else if kind == "write failure" {
				wantState, wantResult, wantCode = "error", "error", "ArchiveIncomplete"
			}
			if (err != nil) != (kind == "write failure") {
				t.Fatal("wrong stream result", err)
			}
			if download.Context().Err() != nil {
				t.Fatal("stream completion cancelled the HTTP writer before its watcher was detached")
			}
			before := plan.Snapshot("")
			for range 3 {
				download.Close()
			}
			if after := plan.Snapshot(""); !after.Finished || after.Cancelled || after.Items[0].State != wantState || after.Items[0] != before.Items[0] {
				t.Fatal("close changed the completed result", before, after)
			}
			if trace.ResultName(download.Context(), 200) != wantResult || trace.Code != wantCode {
				t.Fatal("terminal telemetry changed", trace.Result, trace.Code)
			}
			if download.Context().Err() != context.Canceled {
				t.Fatal("completed download retained its child context")
			}
			if kind != "write failure" {
				reader, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
				if err != nil || len(reader.File) == 0 {
					t.Fatal("completed ZIP cannot be opened", err)
				}
			}
			var duplicate bytes.Buffer
			if err := download.StreamTo(&duplicate, testManifest); err == nil || duplicate.Len() != 0 || source.calls.Load() != 1 {
				t.Fatal("completed download streamed again", err, duplicate.Len(), source.calls.Load())
			}
		})
	}
}

func TestDownloadCloseInterruptsS3Body(t *testing.T) {
	modified := time.Unix(1700000000, 0).UTC()
	entered, stopped := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"etag"`)
		w.Header().Set("Last-Modified", modified.Format(http.TimeFormat))
		w.Header().Set("Content-Length", "1048576")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.WriteString(w, "data")
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
		close(stopped)
	}))
	t.Cleanup(server.Close)
	client, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	source := &downloadSource{read: client.GetObject}
	item := pendingArchiveItem()
	item.Size = 1048576
	item.Info = objectstate.DeletionSnapshot{ETag: "etag", Size: item.Size, Modified: modified}
	download, plan, trace := startTestDownload(t, context.Background(), source, []archiveplan.ArchiveItem{item})
	done := make(chan error, 1)
	go func() { done <- download.StreamTo(io.Discard, testManifest) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("S3 body read did not start")
	}
	download.Close()
	if err := awaitDownload(t, done); err == nil {
		t.Fatal("interrupted body reported success")
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("S3 response body was not released")
	}
	assertCancelledDownload(t, download, plan, trace)
}

func TestDownloadCloseRacesWithStreamStart(t *testing.T) {
	for range 40 {
		source := &downloadSource{read: func(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error) {
			return nil, minio.ErrorResponse{Code: "NoSuchKey", StatusCode: 404}
		}}
		download, plan, trace := startTestDownload(t, context.Background(), source, []archiveplan.ArchiveItem{pendingArchiveItem()})
		start := make(chan struct{})
		var attempts sync.WaitGroup
		for range 8 {
			attempts.Go(func() { <-start; download.Close() })
			attempts.Go(func() { <-start; _ = download.StreamTo(io.Discard, testManifest) })
		}
		close(start)
		attempts.Wait()
		snapshot := plan.Snapshot("")
		if !snapshot.Finished || source.calls.Load() > 1 {
			t.Fatal("concurrent lifecycle lost completion or streamed twice", snapshot, source.calls.Load())
		}
		wantResult := "partial"
		if snapshot.Cancelled {
			wantResult = "cancelled"
		}
		if trace.ResultName(download.Context(), 200) != wantResult {
			t.Fatal("terminal outcome changed during concurrent close", snapshot, trace.Result)
		}
		if err := plan.PrepareForget(); err != nil {
			t.Fatal("terminal plan cannot be forgotten", err)
		}
	}
}

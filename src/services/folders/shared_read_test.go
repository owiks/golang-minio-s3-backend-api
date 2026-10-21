package folders

import (
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
	"minio-web/api/src/services/folders/share"
)

type sharedSource struct{ *minio.Client }

func (sharedSource) ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error) {
	panic("unexpected list")
}

type sharedTicket struct{ grant *foldershare.FolderShare }

func (s sharedTicket) TakeTicket(string, bool) (foldershare.FolderFileTicket, bool) {
	return foldershare.FolderFileTicket{Share: s.grant, Key: "file", Mode: "download", Expires: time.Now().Add(time.Hour)}, true
}

type sharedAdmission struct{ released atomic.Int32 }

func (a *sharedAdmission) AcquireTransfer(string) (func(), error) {
	return func() { a.released.Add(1) }, nil
}

func sharedReadFixture(t *testing.T, parent context.Context, blocked bool) (*SharedRead, *sharedAdmission, <-chan struct{}) {
	t.Helper()
	entered := make(chan struct{})
	var once sync.Once
	size := 5
	if blocked {
		size = 1048576
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"etag"`)
		w.Header().Set("Last-Modified", "Tue, 20 Oct 2026 10:00:00 GMT")
		w.Header().Set("Content-Length", strconv.Itoa(size))
		if r.Method == "HEAD" {
			return
		}
		_, _ = io.WriteString(w, "hello")
		w.(http.Flusher).Flush()
		once.Do(func() { close(entered) })
		if blocked {
			<-r.Context().Done()
		}
	}))
	t.Cleanup(server.Close)
	client, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{Region: "us-east-1", MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	grant := foldershare.New(foldershare.Config{Owner: "owner", Bucket: "bucket", Expires: time.Now().Add(time.Hour), Reader: sharedSource{client}})
	t.Cleanup(grant.Stop)
	admission := &sharedAdmission{}
	file, err := OpenSharedFile(parent, sharedTicket{grant}, admission, "ticket", true, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(file.Close)
	return file, admission, entered
}

func TestSharedReadCloseCancelsBlockedSDKRead(t *testing.T) {
	file, admission, entered := sharedReadFixture(t, context.Background(), true)
	done := make(chan error, 1)
	go func() { _, err := file.StreamTo(io.Discard); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("SDK read did not start")
	}
	closed := make(chan struct{})
	go func() { file.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close waits for the SDK Read mutex before cancelling")
	}
	select {
	case err := <-done:
		if err == nil || file.Context().Err() == nil || admission.released.Load() != 1 {
			t.Fatal("cancelled stream succeeded or retained admission", err, admission.released.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("SDK reader did not stop")
	}
	file.Close()
	if admission.released.Load() != 1 {
		t.Fatal("admission released twice")
	}
}

type heldSharedWriter struct{ entered, proceed chan struct{} }

func (w heldSharedWriter) Write(p []byte) (int, error) {
	close(w.entered)
	<-w.proceed
	return len(p), nil
}

func TestSharedReadKeepsAdmissionWhileWriterOwnsStream(t *testing.T) {
	file, admission, _ := sharedReadFixture(t, context.Background(), false)
	w := heldSharedWriter{make(chan struct{}), make(chan struct{})}
	var unblock sync.Once
	defer unblock.Do(func() { close(w.proceed) })
	done := make(chan struct{})
	go func() { _, _ = file.StreamTo(w); close(done) }()
	<-w.entered
	file.Close()
	if admission.released.Load() != 0 {
		t.Fatal("active stream lost its admission slot")
	}
	if _, err := file.StreamTo(io.Discard); err == nil {
		t.Fatal("second stream accepted")
	}
	unblock.Do(func() { close(w.proceed) })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream did not finish after writer release")
	}
	if admission.released.Load() != 1 {
		t.Fatal("slot retained after active stream exited")
	}
}

func TestSharedReadNormalCompletionKeepsHTTPContextUntilClose(t *testing.T) {
	file, admission, _ := sharedReadFixture(t, context.Background(), false)
	summary := file.Summary()
	summary.Size, summary.Name = 999, "changed"
	if file.Summary().Size != 5 || file.Summary().Name != "file" {
		t.Fatal("caller changed prepared response metadata")
	}
	n, err := file.StreamTo(io.Discard)
	if err != nil || n != 5 || file.Context().Err() != nil || admission.released.Load() != 0 {
		t.Fatal("normal completion cancelled HTTP watcher before it detached", n, err)
	}
	if _, err := file.StreamTo(io.Discard); err == nil {
		t.Fatal("completed stream restarted")
	}
	file.Close()
	if file.Context().Err() != context.Canceled || admission.released.Load() != 1 {
		t.Fatal("lease cleanup lost")
	}
}

func TestSharedReadEarlyRepeatedCloseAndCancelledParent(t *testing.T) {
	for _, parentCancelled := range []bool{false, true} {
		t.Run(strconv.FormatBool(parentCancelled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			file, admission, _ := sharedReadFixture(t, ctx, false)
			if parentCancelled {
				cancel()
				if _, err := file.StreamTo(io.Discard); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			}
			var jobs sync.WaitGroup
			for range 20 {
				jobs.Go(file.Close)
			}
			jobs.Wait()
			if admission.released.Load() != 1 {
				t.Fatal("duplicate or missing lease release")
			}
			if _, err := file.StreamTo(io.Discard); err == nil || apperror.Public(err).Code != "SharedReadClosed" {
				t.Fatal("closed stream restarted", err)
			}
		})
	}
	var zero SharedRead
	zero.Close()
	if _, err := zero.StreamTo(io.Discard); err == nil {
		t.Fatal("unprepared shared read accepted")
	}
}

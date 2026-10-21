package controllers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/folders/share"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
)

type sharedTransport func(*http.Request) (*http.Response, error)

func (f sharedTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type sharedFileReader struct{ *storage.S3Client }

func (sharedFileReader) ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error) {
	panic("single-file streaming must not enumerate a shared folder")
}

type blockedSharedWriter struct {
	header              http.Header
	entered, unblock    chan struct{}
	entering, releasing sync.Once
}

func (w *blockedSharedWriter) Header() http.Header { return w.header }
func (w *blockedSharedWriter) WriteHeader(int)     {}
func (w *blockedSharedWriter) Write([]byte) (int, error) {
	w.entering.Do(func() { close(w.entered) })
	<-w.unblock
	return 0, os.ErrDeadlineExceeded
}
func (w *blockedSharedWriter) SetWriteDeadline(deadline time.Time) error {
	if !deadline.After(time.Now()) {
		w.releasing.Do(func() { close(w.unblock) })
	}
	return nil
}

func TestSharedFileCancellationInterruptsBlockedResponse(t *testing.T) {
	for _, reason := range []string{"request-cancelled", "share-revoked", "share-expired"} {
		t.Run(reason, func(t *testing.T) {
			transport := sharedTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Status: "200 OK", Request: r, ContentLength: 1, Header: http.Header{"Content-Length": {"1"}, "Content-Type": {"video/mp4"}, "Etag": {`"body"`}, "Last-Modified": {time.Unix(1700000000, 0).UTC().Format(http.TimeFormat)}}, Body: io.NopCloser(strings.NewReader("x"))}, nil
			})
			client, err := minio.New("fixture.example", &minio.Options{Creds: credentials.NewStaticV4("key", "secret", ""), Region: "us-east-1", Transport: transport, MaxRetries: 1})
			if err != nil {
				t.Fatal(err)
			}
			app := runtimesvc.New(config.Config{})
			defer app.Close()
			expires := time.Now().Add(time.Hour)
			if reason == "share-expired" {
				expires = time.Now().Add(500 * time.Millisecond)
			}
			share := foldershare.New(foldershare.Config{Token: "share", Owner: "owner", Bucket: "bucket-test", Prefix: "folder/", Expires: expires, Reader: sharedFileReader{storage.NewS3Client(client)}})
			if err := app.Shares.Add(share); err != nil {
				t.Fatal(err)
			}
			ticket, err := app.Shares.IssueTicket(share, "folder/movie.mp4", "media")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			traced, trace := telemetry.Start(ctx, "shared file")
			writer := &blockedSharedWriter{header: http.Header{}, entered: make(chan struct{}), unblock: make(chan struct{})}
			defer writer.SetWriteDeadline(time.Now())
			done := make(chan any, 1)
			go func() {
				defer func() { done <- recover() }()
				if err := HandleDownloadPublicFolderFile(app, writer, httptest.NewRequest("GET", "/api/shared-folder/file/"+ticket, nil).WithContext(traced), ticket); err != nil {
					panic(err)
				}
			}()
			select {
			case <-writer.entered:
			case result := <-done:
				t.Fatalf("stream did not reach writer: %v", result)
			case <-time.After(2 * time.Second):
				t.Fatal("stream did not start")
			}
			if reason == "share-revoked" {
				share.Stop()
			} else if reason == "request-cancelled" {
				cancel()
			}
			select {
			case result := <-done:
				if result != http.ErrAbortHandler {
					t.Fatalf("expected stream abort, got %v", result)
				}
			case <-time.After(time.Second):
				t.Error("cancelled shared stream remains blocked writing to the client")
				_ = writer.SetWriteDeadline(time.Now())
				<-done
			}
			if trace.ResultName(traced, 200) != "cancelled" {
				t.Error("cancelled response logged as a success or failure")
			}
			active := share.ActiveReads()
			if active != 0 {
				t.Fatal("share read not released", active)
			}
		})
	}
}

package runtime

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	settings "github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/archives/zipread"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/staging"
)

func TestZIPCacheRetainsFileForEntryAfterLeaseClose(t *testing.T) {
	for _, reason := range []string{"shutdown", "logout"} {
		t.Run(reason, func(t *testing.T) {
			var body bytes.Buffer
			writer := zip.NewWriter(&body)
			item, err := writer.Create("file.txt")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(item, "contents")
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			app := New(settings.Config{TemporaryBytes: int64(body.Len())})
			defer app.Close()
			if err := app.StoreSession(&Session{id: "owner", expires: time.Now().Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			file, err := app.Temporary.Create(int64(body.Len()), "minio-zip-lease-*")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if _, err := file.Write(body.Bytes()); err != nil {
				t.Fatal(err)
			}
			index, err := zipread.ReadIndex(file, int64(body.Len()))
			if err != nil {
				t.Fatal(err)
			}
			entry := &storedZIP{File: file, Reader: index, Revision: "revision", SessionID: "owner", Refs: 1}
			app.ZIPs.entries["lease"] = entry
			lease := index.Borrow(entry.Revision, app.ZIPs.release("lease", entry))
			defer lease.Close()
			source, err := lease.OpenEntry(0)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			lease.Close()
			if reason == "shutdown" {
				app.Close()
			} else {
				app.RemoveSession("owner")
			}
			if app.Temporary.Used() != int64(body.Len()) {
				t.Fatal("entry lost its backing file reservation")
			}
			if _, err := os.Stat(file.Name()); err != nil {
				t.Fatal("entry lost its backing file", err)
			}
			actual, err := io.ReadAll(source)
			if err != nil || string(actual) != "contents" {
				t.Fatal(string(actual), err)
			}
			_ = source.Close()
			_ = source.Close()
			if app.Temporary.Used() != 0 {
				t.Fatal("last entry retained cache reservation", app.Temporary.Used())
			}
			if _, err := os.Stat(file.Name()); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("backing file not removed", err)
			}
		})
	}
}

func TestZIPCacheCoalescesDownloadsAndReleasesBudget(t *testing.T) {
	var body bytes.Buffer
	zw := zip.NewWriter(&body)
	f, _ := zw.Create("file.txt")
	_, _ = f.Write([]byte("body"))
	_ = zw.Close()
	var gets, heads atomic.Int32
	entered, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"zip-revision"`)
		w.Header().Set("Last-Modified", time.Unix(1700000000, 0).UTC().Format(http.TimeFormat))
		w.Header().Set("Content-Length", strconv.Itoa(body.Len()))
		if r.Method == "HEAD" {
			heads.Add(1)
			return
		}
		gets.Add(1)
		once.Do(func() { close(entered) })
		<-proceed
		_, _ = w.Write(body.Bytes())
	}))
	defer server.Close()
	client, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{Creds: credentials.NewStaticV4("key", "secret", ""), Region: "us-east-1", BucketLookup: minio.BucketLookupPath, MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	app := New(settings.Config{TemporaryBytes: int64(body.Len())})
	defer app.Close()
	session := &Session{id: "test", sessionCore: &sessionCore{s3: client}}
	if err := app.StoreSession(session); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "http://ui/api/zip", nil)
	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	releases := make(chan func(), 2)
	read := func() {
		defer wg.Done()
		z, e := app.CachedZIP(request.Context(), session, "bucket-test", "archive.zip")
		if e == nil {
			entries, readErr := z.Entries()
			if readErr != nil || len(entries) != 1 {
				t.Error("bad ZIP")
			}
			releases <- z.Close
		}
		errs <- e
	}
	go read()
	<-entered
	go read()
	deadline := time.Now().Add(3 * time.Second)
	for heads.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := app.CachedZIP(cancelled, session, "bucket-test", "archive.zip")
	if e == nil {
		t.Error("cancelled cache request succeeded")
	}
	close(proceed)
	wg.Wait()
	close(errs)
	close(releases)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	for release := range releases {
		release()
		release()
	}
	if gets.Load() != 1 || app.Temporary.Used() != int64(body.Len()) {
		t.Fatal("duplicate download or budget", gets.Load(), app.Temporary.Used())
	}
	app.Close()
	if app.Temporary.Used() != 0 {
		t.Fatal("cache budget retained on close")
	}
}

func TestZIPCacheEvictionHandsFailedCleanupToRuntime(t *testing.T) {
	for _, reason := range []string{"expiry", "logout", "shutdown", "late release"} {
		t.Run(reason, func(t *testing.T) {
			app := New(settings.Config{TemporaryBytes: 4})
			defer app.Close()
			if err := app.StoreSession(&Session{id: "owner", expires: time.Now().Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			f, err := app.Temporary.Create(4, "minio-cache-cleanup-*")
			if err != nil {
				t.Fatal(err)
			}
			name := f.Name()
			if err := os.Remove(name); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(name, 0700); err != nil {
				t.Fatal(err)
			}
			blocker := filepath.Join(name, "blocker")
			if err := os.WriteFile(blocker, []byte("test"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Remove(blocker); _ = os.Remove(name); _ = f.Close() })
			entry := &storedZIP{File: f, SessionID: "owner", Used: time.Now().Add(-time.Hour)}
			app.ZIPs.entries["entry"] = entry
			switch reason {
			case "expiry":
				app.reap(time.Now())
			case "logout":
				app.RemoveSession("owner")
			case "shutdown":
				app.Close()
			case "late release":
				entry.Refs = 1
				release := app.ZIPs.release("entry", entry)
				app.Close()
				if app.Temporary.PendingCleanup().Files != 0 {
					t.Fatal("active reader handed to cleanup prematurely")
				}
				release()
				release()
			}
			if len(app.ZIPs.entries) != 0 || app.Temporary.Used() != 4 || app.Temporary.PendingCleanup() != (staging.CleanupStats{Files: 1, Bytes: 4}) {
				t.Fatal("cache dropped the only resource owner", app.Temporary.PendingCleanup())
			}
			if err := os.Remove(blocker); err != nil {
				t.Fatal(err)
			}
			if reason == "shutdown" || reason == "late release" {
				app.Close() // State.closeOnce must not swallow a cleanup retry.
			} else {
				app.reap(time.Now().Add(time.Minute))
			}
			if app.Temporary.Used() != 0 || app.Temporary.PendingCleanup().Files != 0 {
				t.Fatal("runtime did not finish deferred cleanup")
			}
			if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("cache path still exists", err)
			}
		})
	}
}

func TestZIPCacheCloseDuringDownloadDoesNotPublishOrLeak(t *testing.T) {
	for _, reason := range []string{"shutdown", "session-removed"} {
		t.Run(reason, func(t *testing.T) {
			var body bytes.Buffer
			zw := zip.NewWriter(&body)
			entry, _ := zw.Create("file.txt")
			_, _ = entry.Write([]byte("content"))
			_ = zw.Close()
			entered, proceed := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", `"zip"`)
				w.Header().Set("Last-Modified", time.Unix(1700000000, 0).UTC().Format(http.TimeFormat))
				w.Header().Set("Content-Length", strconv.Itoa(body.Len()))
				if r.Method == "HEAD" {
					return
				}
				close(entered)
				<-proceed
				_, _ = w.Write(body.Bytes())
			}))
			defer server.Close()
			client, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{Creds: credentials.NewStaticV4("key", "secret", ""), Region: "us-east-1", MaxRetries: 1})
			if err != nil {
				t.Fatal(err)
			}
			app := New(settings.Config{TemporaryBytes: int64(body.Len())})
			defer app.Close()
			done := make(chan error, 1)
			session := &Session{id: "test", sessionCore: &sessionCore{s3: client}}
			if err := app.StoreSession(session); err != nil {
				t.Fatal(err)
			}
			go func() {
				z, err := app.CachedZIP(context.Background(), session, "bucket-test", "archive.zip")
				if z != nil {
					z.Close()
				}
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				close(proceed)
				t.Fatal("ZIP download did not start")
			}
			if reason == "shutdown" {
				app.Close()
			} else {
				app.RemoveSession("test")
			}
			close(proceed)
			select {
			case err := <-done:
				if err == nil {
					t.Error("late cache publication after close")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("ZIP load did not finish")
			}
			app.ZIPs.mu.Lock()
			cached := len(app.ZIPs.entries)
			app.ZIPs.mu.Unlock()
			if cached != 0 || app.Temporary.Used() != 0 {
				t.Fatal("late ZIP cache or reservation leaked", cached, app.Temporary.Used())
			}
		})
	}
}

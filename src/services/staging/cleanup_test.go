package staging

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"minio-web/api/src/services/apperror"
)

// A nonempty directory at the owned path makes unlink fail deterministically,
// without requiring permissions tricks that fail when the test runs as root.
func blockCleanup(t *testing.T, b *Budget, size int64) (*File, func(bool)) {
	t.Helper()
	f, err := b.Create(size, "minio-cleanup-test-*")
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
	return f, func(externalRemoval bool) {
		t.Helper()
		if err := os.Remove(blocker); err != nil {
			t.Fatal(err)
		}
		if externalRemoval {
			if err := os.Remove(name); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestFailedCloseTransfersOwnershipUntilReaped(t *testing.T) {
	for _, externalRemoval := range []bool{false, true} {
		t.Run(map[bool]string{false: "unlink retry", true: "removed externally"}[externalRemoval], func(t *testing.T) {
			b := New(8)
			f, unblock := blockCleanup(t, b, 8)
			name := f.Name()
			if err := f.Close(); err == nil {
				t.Fatal("fault did not block unlink")
			}
			if stats := b.PendingCleanup(); stats != (CleanupStats{Files: 1, Bytes: 8}) || b.Used() != 8 || b.Available() != 0 {
				t.Fatal("resource lost or released before unlink", stats, b.Used())
			}
			if _, err := b.Create(1, "should-not-fit-*"); err == nil {
				t.Fatal("failed file reservation was reused")
			}
			unblock(externalRemoval)
			// The caller does not retry File.Close; the owner holds the resource.
			b.Reap(time.Now().Add(time.Minute))
			if b.PendingCleanup() != (CleanupStats{}) || b.Used() != 0 || b.Available() != 8 {
				t.Fatal("owner did not reclaim reservation")
			}
			if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("path still exists", err)
			}
			if err := f.Close(); err != nil || b.Used() != 0 {
				t.Fatal("repeated close released twice", err)
			}
			next, err := b.Create(8, "reclaimed-test-*")
			if err != nil {
				t.Fatal("budget not usable after cleanup", err)
			}
			_ = next.Close()
		})
	}
}

func TestCleanupBackoffAndBoundedFairBatch(t *testing.T) {
	t.Run("backoff", func(t *testing.T) {
		b := New(1)
		f, unblock := blockCleanup(t, b, 1)
		_ = f.Close()
		entry := b.pending[f]
		next := entry.next
		for _, delay := range []time.Duration{2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute, 5 * time.Minute} {
			before := entry.attempts
			b.Reap(next.Add(-time.Nanosecond))
			if entry.attempts != before {
				t.Fatal("backoff ignored")
			}
			b.Reap(next)
			if entry.next != next.Add(delay) || entry.busy || b.Used() != 1 {
				t.Fatal("retry schedule or ownership lost", entry)
			}
			next = entry.next
		}
		unblock(false)
		// A caller may also retry earlier than the scheduled sweep.
		if err := f.Close(); err != nil || b.PendingCleanup().Files != 0 || b.Used() != 0 {
			t.Fatal("manual retry did not clear pending state", err)
		}
		b.Reap(next)
		if b.Used() != 0 {
			t.Fatal("stale retry released reservation twice")
		}
	})
	t.Run("batch", func(t *testing.T) {
		b := New(cleanupBatch + 3)
		for range cleanupBatch + 3 {
			f, unblock := blockCleanup(t, b, 1)
			_ = f.Close()
			unblock(false)
		}
		now := time.Now().Add(time.Minute)
		b.Reap(now)
		if b.PendingCleanup().Files != 3 || b.Used() != 3 {
			t.Fatal("sweep was not bounded", b.PendingCleanup(), b.Used())
		}
		b.Reap(now)
		if b.PendingCleanup().Files != 0 || b.Used() != 0 {
			t.Fatal("remaining due files starved")
		}
	})
	t.Run("persistent oldest does not starve later files", func(t *testing.T) {
		b := New(cleanupBatch + 1)
		f, _ := blockCleanup(t, b, 1)
		_ = f.Close()
		for range cleanupBatch {
			f, unblock := blockCleanup(t, b, 1)
			_ = f.Close()
			unblock(false)
		}
		now := time.Now().Add(time.Minute)
		b.Reap(now)
		b.Reap(now)
		if b.PendingCleanup().Files != 1 || b.Used() != 1 {
			t.Fatal("healthy cleanup starved behind persistent failure")
		}
	})
}

func TestCleanupBackpressureIncludesEmptyFiles(t *testing.T) {
	b := New(0)
	for range maxPendingFiles {
		f, _ := blockCleanup(t, b, 0)
		_ = f.Close()
	}
	if b.PendingCleanup() != (CleanupStats{Files: maxPendingFiles}) {
		t.Fatal("empty pending files not accounted for")
	}
	_, err := b.Create(0, "must-not-create-*")
	if err == nil || apperror.Public(err).Code != "TemporaryStorageFull" {
		t.Fatal("empty files bypass pending-cleanup backpressure", err)
	}
}

func TestConcurrentCloseAndReapReleaseExactlyOnce(t *testing.T) {
	b := New(8)
	f, unblock := blockCleanup(t, b, 8)
	_ = f.Close()
	unblock(false)
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() { _ = f.Close() })
		wg.Go(func() { b.Reap(time.Now().Add(time.Hour)) })
		wg.Go(func() { _ = b.Close() })
	}
	wg.Wait()
	if b.PendingCleanup() != (CleanupStats{}) || b.Used() != 0 {
		t.Fatal("concurrent cleanup lost state or double released", b.PendingCleanup(), b.Used())
	}
}

func TestBudgetShutdownRetainsActiveAndFailedResources(t *testing.T) {
	t.Run("active ownership", func(t *testing.T) {
		b := New(4)
		f, err := b.Create(4, "active-test-*")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := b.Close(); err != nil || b.Used() != 4 {
			t.Fatal("shutdown released active reservation", err)
		}
		if _, err := f.Write([]byte("body")); err != nil {
			t.Fatal("shutdown closed active file", err)
		}
		if _, err := b.Create(0, "closed-test-*"); err == nil || apperror.Public(err).Code != "ServerClosing" {
			t.Fatal("shutdown accepted new file", err)
		}
		if err := f.Close(); err != nil || b.Used() != 0 {
			t.Fatal("late close leaked reservation", err)
		}
	})
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "failed before shutdown", true: "failed after shutdown"}[late], func(t *testing.T) {
			b := New(4)
			f, unblock := blockCleanup(t, b, 4)
			if late {
				if err := b.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.Close(); err == nil {
				t.Fatal("expected failure")
			}
			if err := b.Close(); !errors.Is(err, ErrCleanupPending) || b.Used() != 4 || b.PendingCleanup().Files != 1 {
				t.Fatal("shutdown forgot failed resource", err)
			}
			unblock(false)
			if err := b.Close(); err != nil || b.Used() != 0 || b.PendingCleanup().Files != 0 {
				t.Fatal("shutdown cannot retry cleanup", err)
			}
		})
	}
	t.Run("shutdown drains all batches", func(t *testing.T) {
		b := New(cleanupBatch + 1)
		for range cleanupBatch + 1 {
			f, unblock := blockCleanup(t, b, 1)
			_ = f.Close()
			unblock(false)
		}
		if err := b.Close(); err != nil || b.PendingCleanup().Files != 0 || b.Used() != 0 {
			t.Fatal("shutdown left due files behind batch limit", err)
		}
	})
}

func TestCleanupLogsCountsWithoutPathsOrRawErrors(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)
	b := New(8)
	f, unblock := blockCleanup(t, b, 8)
	_ = f.Close()
	_ = f.Close()
	b.Reap(time.Now().Add(time.Hour))
	if strings.Count(output.String(), "cleanup deferred") != 1 || !strings.Contains(output.String(), `"pending_files":1`) || !strings.Contains(output.String(), `"pending_bytes":8`) {
		t.Fatal("missing counts or repeated warning", output.String())
	}
	if strings.Contains(output.String(), f.Name()) || strings.Contains(output.String(), "directory not empty") {
		t.Fatal("unsafe filesystem details in log")
	}
	unblock(true)
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
}

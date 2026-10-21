package zipread

import (
	"archive/zip"
	"bytes"
	"io"
	"sync"
	"sync/atomic"
	"testing"
)

func leaseTestIndex(t *testing.T) *Index {
	t.Helper()
	var body bytes.Buffer
	w := zip.NewWriter(&body)
	f, err := w.Create("folder/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(f, "contents"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	index, err := ReadIndex(bytes.NewReader(body.Bytes()), int64(body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return index
}

func TestArchiveDirectorySnapshotsCannotChangeOtherReaders(t *testing.T) {
	index := leaseTestIndex(t)
	first, second := index.Borrow("revision", nil), index.Borrow("revision", nil)
	defer first.Close()
	defer second.Close()
	want, err := second.Entry(0)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := first.Entries()
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	entries[0] = Entry{Index: 99, Name: "../unsafe", Size: 1 << 60, Flags: 1, Method: 99}
	entry, err := first.Entry(0)
	if err != nil {
		t.Fatal(err)
	}
	entry.Name = "changed"
	for _, lease := range []*Archive{first, second} {
		got, err := lease.Entry(0)
		if err != nil || got != want || lease.Revision() != "revision" {
			t.Fatal("shared directory changed", got, want, err)
		}
		r, err := lease.OpenEntry(0)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil || string(body) != "contents" {
			t.Fatal(string(body), err)
		}
	}
}

func TestArchiveClosedLeaseRetainsBackingUntilLastEntryCloses(t *testing.T) {
	var released atomic.Int32
	lease := leaseTestIndex(t).Borrow("revision", func() { released.Add(1) })
	first, err := lease.OpenEntry(0)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := lease.OpenEntry(0)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	lease.Close()
	lease.Close()
	if released.Load() != 0 {
		t.Fatal("backing file released while readers are active")
	}
	if _, err := lease.Entries(); err == nil {
		t.Fatal("closed directory is readable")
	}
	if _, err := lease.Entry(0); err == nil {
		t.Fatal("closed entry is readable")
	}
	if _, err := lease.OpenEntry(0); err == nil {
		t.Fatal("closed lease opened a reader")
	}
	for _, source := range []io.ReadCloser{first, second} {
		body, err := io.ReadAll(source)
		if err != nil || string(body) != "contents" {
			t.Fatal("active entry was interrupted by parent Close", string(body), err)
		}
	}
	_ = first.Close()
	_ = first.Close()
	if released.Load() != 0 {
		t.Fatal("last reader still owns backing file")
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() { _ = second.Close(); lease.Close() })
	}
	wg.Wait()
	if released.Load() != 1 {
		t.Fatal("backing file released more than once", released.Load())
	}
	if _, err := second.Read(make([]byte, 1)); err != io.ErrClosedPipe {
		t.Fatal("read after Close", err)
	}
}

func TestArchiveConcurrentBorrowReadAndClose(t *testing.T) {
	index := leaseTestIndex(t)
	var released atomic.Int32
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			lease := index.Borrow("revision", func() { released.Add(1) })
			source, err := lease.OpenEntry(0)
			if err != nil {
				t.Error(err)
				lease.Close()
				return
			}
			var children sync.WaitGroup
			children.Go(func() {
				_, _ = io.Copy(io.Discard, source)
				_ = source.Close()
			})
			children.Go(func() { _ = source.Close(); lease.Close() })
			children.Go(func() { _, _ = lease.Entries(); _, _ = lease.Entry(0) })
			children.Wait()
			lease.Close()
		})
	}
	wg.Wait()
	if released.Load() != 64 {
		t.Fatal("lost or duplicate leases", released.Load())
	}
}

func TestArchiveRejectsInvalidEntriesAndUnpreparedLease(t *testing.T) {
	lease := leaseTestIndex(t).Borrow("revision", nil)
	defer lease.Close()
	for _, index := range []int{-1, 1, 1000} {
		if _, err := lease.Entry(index); err == nil {
			t.Fatal("invalid entry accepted", index)
		}
		if _, err := lease.OpenEntry(index); err == nil {
			t.Fatal("invalid entry opened", index)
		}
	}
	var zero Archive
	if _, err := zero.Entries(); err == nil {
		t.Fatal("unprepared archive accepted")
	}
	zero.Close()
	zero.Close()
}

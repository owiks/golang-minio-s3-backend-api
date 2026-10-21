package zipread

import (
	"archive/zip"
	"io"
	"os"
	"sync"

	"minio-web/api/src/services/apperror"
)

// Index owns the parsed directory. Neither the shared zip.Reader nor its mutable
// zip.File headers cross the cache boundary.
type Index struct{ reader *zip.Reader }

func ReadIndex(source io.ReaderAt, size int64) (*Index, error) {
	reader, err := CheckedZIP(source, size)
	if err != nil {
		return nil, err
	}
	return &Index{reader: reader}, nil
}

type Entry struct {
	Index            int
	Name             string
	Size, Compressed uint64
	Mode             os.FileMode
	Flags, Method    uint16
}

func entryInfo(index int, f *zip.File) Entry {
	return Entry{Index: index, Name: f.Name, Size: f.UncompressedSize64, Compressed: f.CompressedSize64, Mode: f.Mode(), Flags: f.Flags, Method: f.Method}
}

// Archive is one cache lease. Closing it prevents new reads; already opened
// entries retain the backing file until their own Close completes.
type Archive struct {
	mu       sync.Mutex
	index    *Index
	revision string
	release  func()
	closed   bool
	readers  int
}

func (i *Index) Borrow(revision string, release func()) *Archive {
	return &Archive{index: i, revision: revision, release: release}
}

func (a *Archive) Revision() string { return a.revision }

func closedArchive() error {
	return apperror.New(409, "ZIPClosed", "Чтение архива закрыто. Откройте архив заново.")
}

func invalidEntry() error {
	return apperror.New(400, "InvalidZIPEntry", "Запись архива не найдена.")
}

func (a *Archive) Entries() ([]Entry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.index == nil {
		return nil, closedArchive()
	}
	entries := make([]Entry, len(a.index.reader.File))
	for n, f := range a.index.reader.File {
		entries[n] = entryInfo(n, f)
	}
	return entries, nil
}

func (a *Archive) Entry(index int) (Entry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.index == nil {
		return Entry{}, closedArchive()
	}
	if index < 0 || index >= len(a.index.reader.File) {
		return Entry{}, invalidEntry()
	}
	return entryInfo(index, a.index.reader.File[index]), nil
}

func (a *Archive) OpenEntry(index int) (io.ReadCloser, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.index == nil {
		return nil, closedArchive()
	}
	if index < 0 || index >= len(a.index.reader.File) {
		return nil, invalidEntry()
	}
	source, err := a.index.reader.File[index].Open()
	if err != nil {
		return nil, err
	}
	a.readers++
	return &entryReader{source: source, owner: a}, nil
}

func (a *Archive) releaseLocked() func() {
	if !a.closed || a.readers != 0 {
		return nil
	}
	release := a.release
	a.index, a.release = nil, nil
	return release
}

func (a *Archive) Close() {
	a.mu.Lock()
	a.closed = true
	release := a.releaseLocked()
	a.mu.Unlock()
	if release != nil {
		release()
	}
}

type entryReader struct {
	mu     sync.Mutex
	source io.ReadCloser
	owner  *Archive
	closed bool
}

func (r *entryReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	return r.source.Read(p)
}

func (r *entryReader) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	err := r.source.Close()
	r.mu.Unlock()
	a := r.owner
	a.mu.Lock()
	a.readers--
	release := a.releaseLocked()
	a.mu.Unlock()
	if release != nil {
		release()
	}
	return err
}

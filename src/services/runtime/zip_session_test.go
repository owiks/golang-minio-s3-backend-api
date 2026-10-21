package runtime

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"minio-web/api/src/config"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/archives/zipread"
	"minio-web/api/src/services/objects/state"
)

type cachedSessionSource struct {
	info             minio.ObjectInfo
	heads            atomic.Int32
	entered, proceed chan struct{}
}

func (*cachedSessionSource) SessionID() string { return "owner" }
func (s *cachedSessionSource) StatObject(ctx context.Context, _ string, _ string, _ minio.StatObjectOptions) (minio.ObjectInfo, error) {
	s.heads.Add(1)
	if s.entered != nil {
		close(s.entered)
		select {
		case <-s.proceed:
		case <-ctx.Done():
			return minio.ObjectInfo{}, ctx.Err()
		}
	}
	return s.info, nil
}
func (*cachedSessionSource) GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error) {
	panic("cache should never fetch another ZIP")
}

func sessionCacheFixture(t *testing.T) (*State, *zipread.Archive, *cachedSessionSource, int64) {
	t.Helper()
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
	size := int64(body.Len())
	app := New(config.Config{TemporaryBytes: size})
	t.Cleanup(app.Close)
	if err := app.StoreSession(&Session{id: "owner", expires: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	file, err := app.Temporary.Create(size, "minio-session-cache-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if _, err := file.Write(body.Bytes()); err != nil {
		t.Fatal(err)
	}
	index, err := zipread.ReadIndex(file, size)
	if err != nil {
		t.Fatal(err)
	}
	source := &cachedSessionSource{info: minio.ObjectInfo{ETag: "etag", Size: size, LastModified: time.Unix(1700000000, 0).UTC()}}
	rev := objectstate.DeletionRevision(objectstate.DeletionSnapshot{ETag: source.info.ETag, Size: size, Modified: source.info.LastModified})
	id := "owner\x00bucket-test\x00archive.zip\x00" + rev
	entry := &storedZIP{File: file, Reader: index, Revision: rev, SessionID: "owner", Refs: 1, Used: time.Now()}
	app.ZIPs.entries[id] = entry
	lease := index.Borrow(rev, app.ZIPs.release(id, entry))
	t.Cleanup(lease.Close)
	return app, lease, source, size
}

func TestZIPCacheRejectsNewHitAfterLogoutAndKeepsExistingReader(t *testing.T) {
	for _, duringStat := range []bool{false, true} {
		t.Run(map[bool]string{false: "before open", true: "during stat"}[duringStat], func(t *testing.T) {
			app, lease, source, size := sessionCacheFixture(t)
			entry, err := lease.OpenEntry(0)
			if err != nil {
				t.Fatal(err)
			}
			defer entry.Close()
			done := make(chan error, 1)
			if duringStat {
				source.entered, source.proceed = make(chan struct{}), make(chan struct{})
				go func() {
					next, err := app.ZIPs.Open(context.Background(), source, "bucket-test", "archive.zip")
					if next != nil {
						next.Close()
					}
					done <- err
				}()
				select {
				case <-source.entered:
				case <-time.After(time.Second):
					t.Fatal("stat not entered")
				}
			}
			app.RemoveSession("owner")
			if duringStat {
				close(source.proceed)
			} else {
				next, err := app.ZIPs.Open(context.Background(), source, "bucket-test", "archive.zip")
				if next != nil {
					next.Close()
				}
				done <- err
			}
			select {
			case err := <-done:
				if apperror.Public(err).Code != "SessionExpired" {
					t.Fatal("logout accepted a new lease", err)
				}
			case <-time.After(time.Second):
				t.Fatal("cache request blocked")
			}
			wantHeads := int32(0)
			if duringStat {
				wantHeads = 1
			}
			if source.heads.Load() != wantHeads {
				t.Fatal("unexpected upstream after logout", source.heads.Load())
			}
			lease.Close()
			if app.Temporary.Used() != size {
				t.Fatal("old reader lost backing file")
			}
			raw, err := io.ReadAll(entry)
			if err != nil || string(raw) != "contents" {
				t.Fatal("logout interrupted admitted reader", string(raw), err)
			}
			_ = entry.Close()
			if app.Temporary.Used() != 0 {
				t.Fatal("last old reader retained budget")
			}
		})
	}
}

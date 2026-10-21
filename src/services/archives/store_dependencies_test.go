package archives

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/archives/plan"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/archives/zipread"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/staging"
)

type archivePublisher struct {
	ctx     context.Context
	plan    *archiveplan.ArchivePlan
	failure error
}

func (p *archivePublisher) Put(ctx context.Context, plan *archiveplan.ArchivePlan) (string, error) {
	p.ctx, p.plan = ctx, plan
	return "ticket", p.failure
}

type archiveSource struct{ t *testing.T }

func (s *archiveSource) SessionID() string { return "owner" }
func (s *archiveSource) StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error) {
	return minio.ObjectInfo{ETag: "source-etag", Size: 4, LastModified: time.Unix(1700000000, 0).UTC()}, nil
}
func (s *archiveSource) ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error) {
	s.t.Fatal("unexpected recursive listing")
	return minio.ListBucketV2Result{}, nil
}
func (s *archiveSource) GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error) {
	s.t.Fatal("unexpected body read")
	return nil, nil
}
func (s *archiveSource) PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error) {
	s.t.Fatal("unexpected object write")
	return minio.UploadInfo{}, nil
}

func TestPrepareArchivePublishesThroughOnlyPut(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &archiveSource{t: t}
	request := PrepareArchiveRequest{Bucket: "test-bucket", Keys: []string{"folder/file.txt"}, Base: "folder/"}
	store := &archivePublisher{}
	value, err := PrepareArchive(ctx, store, source, request)
	if err != nil || store.ctx != ctx || store.plan == nil || store.plan.SessionID() != "owner" || store.plan.Snapshot("").Items[0].Info.ETag != "source-etag" || value.Ticket != "ticket" || value.Count != 1 || value.Size != 4 || value.Items[0].Path != "file.txt" {
		t.Fatal("publication scope/snapshot lost", value, err)
	}
	failure := errors.New("publication rejected")
	store = &archivePublisher{failure: failure}
	if _, err = PrepareArchive(ctx, store, source, request); !errors.Is(err, failure) {
		t.Fatal("publication failure lost", err)
	}
	cancel()
	store = &archivePublisher{}
	if _, err = PrepareArchive(ctx, store, source, request); !errors.Is(err, context.Canceled) || store.plan != nil {
		t.Fatal("cancelled plan published", err)
	}
}

type archiveLookup struct {
	plan          *archiveplan.ArchivePlan
	ticket, owner string
}

func (s *archiveLookup) Get(ticket, owner string) *archiveplan.ArchivePlan {
	s.ticket, s.owner = ticket, owner
	return s.plan
}

type archiveForgetter struct {
	*archiveLookup
	forgotten *archiveplan.ArchivePlan
}

func (s *archiveForgetter) Forget(_ string, p *archiveplan.ArchivePlan) { s.forgotten = p }

func TestArchiveLifecycleUsesReadAndForgetPorts(t *testing.T) {
	plan := archiveplan.New(archiveplan.Config{SessionID: "owner", Created: time.Now(), Name: "test.zip", Items: []archiveplan.ArchiveItem{{Key: "file", State: "pending", Size: 4}}})
	lookup := &archiveLookup{plan: plan}
	download, err := StartDownload(context.Background(), lookup, &archiveSource{t: t}, "ticket")
	if err != nil || lookup.owner != "owner" || lookup.ticket != "ticket" || !plan.Snapshot("").Started {
		t.Fatal("read port lost owner or start state", err)
	}
	defer download.Close()
	forgetter := &archiveForgetter{archiveLookup: lookup}
	if _, err = ForgetArchive(forgetter, "owner", "ticket"); err == nil || apperror.Public(err).Code != "ArchiveRunning" || forgetter.forgotten != nil {
		t.Fatal("running plan forgotten", err)
	}
	value, err := CancelArchive(lookup, "owner", "ticket")
	if err != nil || !value.Cancelled || download.Context().Err() != context.Canceled {
		t.Fatal("cancel not propagated", err)
	}
	download.Close()
	if _, err = ForgetArchive(forgetter, "owner", "ticket"); err != nil || forgetter.forgotten != plan {
		t.Fatal("forget lost plan identity", err)
	}
	for _, kind := range []string{"foreign-owner", "expired", "missing"} {
		t.Run(kind, func(t *testing.T) {
			config := archiveplan.Config{SessionID: "owner", Created: time.Now()}
			switch kind {
			case "foreign-owner":
				config.SessionID = "other"
			case "expired":
				config.Created = time.Now().Add(-2 * time.Hour)
			case "missing":
				// No plan is returned for a missing ticket.
			}
			p := archiveplan.New(config)
			if kind == "missing" {
				p = nil
			}
			if _, err := ArchiveStatus(&archiveLookup{plan: p}, "owner", "ticket"); err == nil || apperror.Public(err).Code != "ArchiveExpired" {
				t.Fatal("unavailable plan exposed", err)
			}
		})
	}
}

type zipLease struct {
	reader      *zipread.Index
	ctx         context.Context
	source      zipread.ZIPSource
	bucket, key string
	releases    int
	failure     error
}

func (c *zipLease) Open(ctx context.Context, s zipread.ZIPSource, bucket, key string) (*zipread.Archive, error) {
	c.ctx, c.source, c.bucket, c.key = ctx, s, bucket, key
	if c.failure != nil {
		return nil, c.failure
	}
	return c.reader.Borrow("zip-revision", func() { c.releases++ }), nil
}

func TestZIPCachePortPreservesLeaseOnSuccessAndRejection(t *testing.T) {
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	entry, err := writer.Create("file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write([]byte("body")); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zipread.ReadIndex(bytes.NewReader(data.Bytes()), int64(data.Len()))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	source := &archiveSource{t: t}
	cache := &zipLease{reader: reader}
	value, err := InspectZIP(ctx, cache, source, "test-bucket", "archive.zip")
	if err != nil || value.Revision != "zip-revision" || len(value.Items) != 1 || value.Items[0].Name != "file.txt" || cache.ctx != ctx || cache.source != source || cache.bucket != "test-bucket" || cache.key != "archive.zip" || cache.releases != 1 {
		t.Fatal("ZIP lease/scope lost", value, err)
	}
	_, err = ExtractZIPEntry(ctx, cache, staging.New(4), source, "test-bucket", "archive.zip", ZipExplorerRequest{Bucket: "test-bucket", Key: "file.txt", Revision: "stale"})
	if err == nil || apperror.Public(err).Code != "ZIPChanged" || cache.releases != 2 {
		t.Fatal("rejected extraction leaked lease", err)
	}
	failure := errors.New("cache unavailable")
	cache.failure = failure
	if _, err = InspectZIP(ctx, cache, source, "test-bucket", "archive.zip"); !errors.Is(err, failure) || cache.releases != 2 {
		t.Fatal("cache failure altered lease", err)
	}
}

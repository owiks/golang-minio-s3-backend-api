package archives

import (
	"context"
	"io"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/archives/plan"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/archives/zipread"
)

// Interfaces belong to the use cases that consume them. Only required
// operations are exposed; connection credentials and session state stay outside.
type PrepareArchiveStore interface {
	Put(context.Context, *archiveplan.ArchivePlan) (string, error)
}

type ReadArchiveStore interface {
	Get(ticket, owner string) *archiveplan.ArchivePlan
}

type ForgetArchiveStore interface {
	ReadArchiveStore
	Forget(string, *archiveplan.ArchivePlan)
}

// Open lends a closed-state-aware archive lease, never the cached ZIP directory.
type ZIPArchiveCache interface {
	Open(context.Context, zipread.ZIPSource, string, string) (*zipread.Archive, error)
}

type ExtractZIPEntryClient interface {
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
	SessionID() string
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type InspectZIPClient interface {
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
	SessionID() string
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type PrepareArchiveClient interface {
	ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error)
	SessionID() string
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type StartDownloadClient interface {
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
	SessionID() string
}

type DownloadReader interface {
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
}

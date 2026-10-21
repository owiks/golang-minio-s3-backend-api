package folders

import (
	"context"
	"io"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/folders/share"
	"minio-web/api/src/services/iam"
)

// Interfaces belong to the use cases that consume them. Only required
// operations are exposed; connection credentials and session state stay outside.
type FolderShareLister interface {
	List(owner, bucket, prefix string) []foldershare.FolderShareSummary
}

type FolderShareRevoker interface {
	Revoke(owner, token string) error
}

type FolderSharePublisher interface {
	Add(*foldershare.FolderShare) error
}

type PublicFolderStore interface {
	Get(token string) *foldershare.FolderShare
	IssueTicket(*foldershare.FolderShare, string, string) (string, error)
}

type SharedFileTickets interface {
	TakeTicket(id string, consume bool) (foldershare.FolderFileTicket, bool)
}

type CreateFolderClient interface {
	ListObjects(context.Context, string, minio.ListObjectsOptions) <-chan minio.ObjectInfo
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
}

type CreateFolderShareClient interface {
	ActionPolicySnapshot() *iam.Policy
	ActorName() string
	GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error)
	RefreshActionPolicy(context.Context) *iam.ActionCheckIssue
	ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error)
	SessionID() string
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type DeleteFolderObjectClient interface {
	ListObjects(context.Context, string, minio.ListObjectsOptions) <-chan minio.ObjectInfo
	RemoveObject(context.Context, string, string, minio.RemoveObjectOptions) error
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type FolderDeleteItemsClient interface {
	ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error)
}

type FolderExistsClient interface {
	ListObjects(context.Context, string, minio.ListObjectsOptions) <-chan minio.ObjectInfo
}

type InspectFolderObjectClient interface {
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

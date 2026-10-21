package uploads

import (
	"context"
	"io"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/iam"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads/invite"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads/source"
)

// Interfaces belong to the use cases that consume them. Only required
// operations are exposed; connection credentials and session state stay outside.
type UploadInviteLister interface {
	List(owner, bucket, prefix string) []uploadinvite.UploadInviteSummary
}

type UploadInviteRevoker interface {
	Revoke(owner, token string) error
}

type UploadInvitePublisher interface {
	Add(*uploadinvite.UploadInvite) error
}

type UploadInviteReader interface {
	Get(token string) *uploadinvite.UploadInvite
}

type AbortIncompleteUploadClient interface {
	AbortMultipartUpload(context.Context, string, string, string) error
	ListObjectParts(context.Context, string, string, string, int, int) (minio.ListObjectPartsResult, error)
}

type CreateUploadInviteClient interface {
	ActionPolicySnapshot() *iam.Policy
	ActorName() string
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
	RefreshActionPolicy(context.Context) *iam.ActionCheckIssue
	SessionID() string
}

type EvaluateInviteScopeClient interface {
	ActionPolicySnapshot() *iam.Policy
	ActorName() string
	RefreshActionPolicy(context.Context) *iam.ActionCheckIssue
}

type ImportClient interface {
	PutObject(context.Context, string, string, io.Reader, int64, minio.PutObjectOptions) (minio.UploadInfo, error)
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

// RemoteSource opens a validated source and transfers ownership of its body.
// Implementations must enforce source access restrictions and context cancellation.
type RemoteSource interface {
	Open(context.Context, string) (source.File, error)
}

type InspectIncompleteUploadClient interface {
	ListObjectParts(context.Context, string, string, string, int, int) (minio.ListObjectPartsResult, error)
}

type ListIncompleteUploadsClient interface {
	ListMultipartUploads(context.Context, string, string, string, string, string, int) (minio.ListMultipartUploadsResult, error)
	ListObjectParts(context.Context, string, string, string, int, int) (minio.ListObjectPartsResult, error)
}

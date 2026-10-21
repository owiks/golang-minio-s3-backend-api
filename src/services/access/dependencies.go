package access

import (
	"context"
	"encoding/json"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/iam"
)

// Interfaces belong to the use cases that consume them. Only required
// operations are exposed; connection credentials and session state stay outside.
type AccessibleBucketsClient interface {
	AccountInfo(context.Context, madmin.AccountOpts) (madmin.AccountInfo, error)
	ListBuckets(context.Context) ([]minio.BucketInfo, error)
	ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error)
}

type DiscoverAccessClient interface {
	ProbeDiagnostic(context.Context, bool) error
	AccountInfo(context.Context, madmin.AccountOpts) (madmin.AccountInfo, error)
	DataUsageInfo(context.Context) (madmin.DataUsageInfo, error)
	GetConfigKVWithOptions(context.Context, string, madmin.KVOptions) ([]byte, error)
	ListAccessKeysBulk(context.Context, []string, madmin.ListAccessKeysOpts) (map[string]madmin.ListAccessKeysResp, error)
	ListBatchJobs(context.Context, *madmin.ListBatchJobsFilter) (madmin.ListBatchJobsResult, error)
	ListBuckets(context.Context) ([]minio.BucketInfo, error)
	ListCannedPolicies(context.Context) (map[string]json.RawMessage, error)
	ListGroups(context.Context) ([]string, error)
	ListMultipartUploads(context.Context, string, string, string, string, string, int) (minio.ListMultipartUploadsResult, error)
	ListPoolsStatus(context.Context) ([]madmin.PoolStatus, error)
	ListServiceAccounts(context.Context, string) (madmin.ListServiceAccountsResp, error)
	ListTiers(context.Context) ([]*madmin.TierConfig, error)
	ListUsers(context.Context) (map[string]madmin.UserInfo, error)
	LockAccess(context.Context) (func(), error)
	ReadS3Metrics(context.Context) ([]byte, error)
	RebalanceStatus(context.Context) (madmin.RebalanceStatus, error)
	RefreshActionPolicy(context.Context) *iam.ActionCheckIssue
	ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error)
	ServerInfo(context.Context, ...func(*madmin.ServerInfoOpts)) (madmin.InfoMessage, error)
}

type ProbeDiagnosticClient interface {
	ProbeDiagnostic(context.Context, bool) error
}

package buckets

import (
	"context"
	"encoding/json"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/cors"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
	"github.com/minio/minio-go/v7/pkg/sse"
	"github.com/minio/minio-go/v7/pkg/tags"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

// Interfaces belong to the use cases that consume them. Only required
// operations are exposed; connection credentials and session state stay outside.
type ReadLifecycleClient interface {
	ReadLifecycleConfig(context.Context, string) (storage.ConfigRead[*lifecycle.Configuration], error)
}
type ChangeLifecycleClient interface {
	ReadLifecycleClient
	WriteLifecycleConfig(context.Context, string, *lifecycle.Configuration) (int, error)
}
type ReadBrowserSettingsClient interface {
	ReadCORSConfig(context.Context, string) (storage.ConfigRead[*cors.Config], error)
	ReadEncryptionConfig(context.Context, string) (storage.ConfigRead[*sse.Configuration], error)
}
type ChangeBrowserSettingsClient interface {
	ReadBrowserSettingsClient
	WriteCORSConfig(context.Context, string, *cors.Config) (int, error)
	WriteEncryptionConfig(context.Context, string, *sse.Configuration) (int, error)
}

type ChangeRetentionClient interface {
	GetBucketPolicy(context.Context, string) (string, error)
	GetBucketQuota(context.Context, string) (madmin.BucketQuota, error)
	GetBucketTagging(context.Context, string) (*tags.Tags, error)
	GetBucketVersioning(context.Context, string) (minio.BucketVersioningConfiguration, error)
	GetObjectLockConfig(context.Context, string) (string, *minio.RetentionMode, *uint, *minio.ValidityUnit, error)
	SetBucketObjectLockConfig(context.Context, string, *minio.RetentionMode, *uint, *minio.ValidityUnit) error
	SetBucketVersioning(context.Context, string, minio.BucketVersioningConfiguration) error
}

type CreateBucketClient interface {
	BucketExists(context.Context, string) (bool, error)
	MakeBucket(context.Context, string, minio.MakeBucketOptions) error
	RegionName() string
}

type DeleteBucketClient interface {
	RemoveBucket(context.Context, string) error
}

type PolicyChangeClient interface {
	ReadPolicyClient
	SetBucketPolicy(context.Context, string, string) error
}

type QuotaChangeClient interface {
	ReadQuotaClient
	SetBucketQuota(context.Context, string, *madmin.BucketQuota) error
}

type TagsChangeClient interface {
	ReadTagsClient
	RemoveBucketTagging(context.Context, string) error
	SetBucketTagging(context.Context, string, *tags.Tags) error
}

type ReadPolicyClient interface {
	GetBucketPolicy(context.Context, string) (string, error)
}

type ReadQuotaClient interface {
	GetBucketQuota(context.Context, string) (madmin.BucketQuota, error)
}

type ReadTagsClient interface {
	GetBucketTagging(context.Context, string) (*tags.Tags, error)
}

type ReadBucketAccessClient interface {
	GetBucketPolicy(context.Context, string) (string, error)
	GetGroupDescription(context.Context, string) (*madmin.GroupDesc, error)
	GetPolicyEntities(context.Context, madmin.PolicyEntitiesQuery) (madmin.PolicyEntitiesResult, error)
	InfoServiceAccount(context.Context, string) (madmin.InfoServiceAccountResp, error)
	ListAccessKeysBulk(context.Context, []string, madmin.ListAccessKeysOpts) (map[string]madmin.ListAccessKeysResp, error)
	ListCannedPolicies(context.Context) (map[string]json.RawMessage, error)
	ListGroups(context.Context) ([]string, error)
	ListUsers(context.Context) (map[string]madmin.UserInfo, error)
}

type ReadBucketSettingClient interface {
	ReadPolicyClient
	ReadQuotaClient
	ReadTagsClient
	ReadVersioningClient
	ReadLockingClient
}

type ReadDetailsClient interface {
	GetBucketPolicy(context.Context, string) (string, error)
	GetBucketQuota(context.Context, string) (madmin.BucketQuota, error)
	GetBucketTagging(context.Context, string) (*tags.Tags, error)
	GetBucketVersioning(context.Context, string) (minio.BucketVersioningConfiguration, error)
	GetObjectLockConfig(context.Context, string) (string, *minio.RetentionMode, *uint, *minio.ValidityUnit, error)
	ReadLifecycleClient
	ReadBrowserSettingsClient
}

type ReadLockingClient interface {
	GetObjectLockConfig(context.Context, string) (string, *minio.RetentionMode, *uint, *minio.ValidityUnit, error)
}

type ReadUsageClient interface {
	AccountInfo(context.Context, madmin.AccountOpts) (madmin.AccountInfo, error)
	DataUsageInfo(context.Context) (madmin.DataUsageInfo, error)
	ListBuckets(context.Context) ([]minio.BucketInfo, error)
	ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error)
}

type ReadVersioningClient interface {
	GetBucketVersioning(context.Context, string) (minio.BucketVersioningConfiguration, error)
}

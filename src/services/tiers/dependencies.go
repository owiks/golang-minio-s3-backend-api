package tiers

import (
	"context"
	"net/url"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/lifecycle"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/iam"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

// Interfaces belong to the use cases that consume them. Only required
// operations are exposed; connection credentials and session state stay outside.
type CreateTierClient interface {
	ActionPolicySnapshot() *iam.Policy
	ActorName() string
	AddTier(context.Context, *madmin.TierConfig) error
	EditTier(context.Context, string, madmin.TierCreds) error
	InternalEndpoint() *url.URL
	ListBuckets(context.Context) ([]minio.BucketInfo, error)
	ListTiers(context.Context) ([]*madmin.TierConfig, error)
	ReadLifecycleConfig(context.Context, string) (storage.ConfigRead[*lifecycle.Configuration], error)
	PublicEndpoint() *url.URL
	RefreshActionPolicy(context.Context) *iam.ActionCheckIssue
	RemoveTier(context.Context, string) error
	VerifyTier(context.Context, string) error
}

type ListTiersClient interface {
	ListTiers(context.Context) ([]*madmin.TierConfig, error)
}

type UnusedTierClient interface {
	ActionPolicySnapshot() *iam.Policy
	ActorName() string
	ListBuckets(context.Context) ([]minio.BucketInfo, error)
	ReadLifecycleConfig(context.Context, string) (storage.ConfigRead[*lifecycle.Configuration], error)
}

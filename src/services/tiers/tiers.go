package tiers

import (
	"context"
	"encoding/json"

	"github.com/minio/madmin-go/v3"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	bucketssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/buckets"
	endpoints "github.com/owiks/golang-minio-s3-backend-api/src/services/endpoint"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/iam"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/revision"
)

type TierView struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Endpoint     string `json:"endpoint"`
	Bucket       string `json:"bucket"`
	Prefix       string `json:"prefix"`
	Region       string `json:"region"`
	StorageClass string `json:"storageClass"`
	Editable     bool   `json:"editable"`
	Revision     string `json:"revision"`
}

func TierSummary(c *madmin.TierConfig) TierView {
	v := TierView{Name: c.Name, Type: c.Type.String()}
	switch c.Type {
	case madmin.MinIO:
		if c.MinIO != nil {
			v.Endpoint = c.MinIO.Endpoint
			v.Bucket = c.MinIO.Bucket
			v.Prefix = c.MinIO.Prefix
			v.Region = c.MinIO.Region
			v.Editable = true
		}
	case madmin.S3:
		if c.S3 != nil {
			v.Endpoint = c.S3.Endpoint
			v.Bucket = c.S3.Bucket
			v.Prefix = c.S3.Prefix
			v.Region = c.S3.Region
			v.StorageClass = c.S3.StorageClass
			v.Editable = !c.S3.AWSRole && c.S3.AWSRoleWebIdentityTokenFile == ""
		}
	case madmin.Azure:
		if c.Azure != nil {
			v.Bucket = c.Azure.Bucket
			v.Prefix = c.Azure.Prefix
		}
	case madmin.GCS:
		if c.GCS != nil {
			v.Bucket = c.GCS.Bucket
			v.Prefix = c.GCS.Prefix
		}
	}
	if v.Endpoint != "" {
		if u, e := endpoints.Parse(v.Endpoint); e == nil {
			v.Endpoint = u.String()
		} else {
			v.Endpoint = ""
		}
	}
	raw, _ := json.Marshal(v)
	v.Revision = revision.Of(raw)
	return v
}
func TierError(err error) error {
	if err == nil {
		return nil
	}
	p := apperror.Public(err)
	if p.Status != 502 {
		return p
	}
	return apperror.New(502, "TierOperationFailed", messages.TiersTierOperationFailed)
}

// Native RemoveTier protects remote data; also refuse to orphan lifecycle rules.
func UnusedTier(ctx context.Context, s UnusedTierClient, name string) error {
	policy := s.ActionPolicySnapshot()
	allowed := policy.Allows(s.ActorName(), iam.Query{Action: "s3:ListAllMyBuckets"})
	if !allowed {
		return apperror.New(403, "TierUsageUnknown", messages.TiersTierUsageUnknown)
	}
	buckets, e := s.ListBuckets(ctx)
	if e != nil {
		return e
	}
	for _, bucket := range buckets {
		state, e := bucketssvc.ReadLifecycle(ctx, s, bucket.Name)
		if e != nil {
			return apperror.New(409, "TierUsageUnknown", messages.TiersTierUsageUnknownTiers)
		}
		if !state.View.Writable {
			return apperror.New(409, "TierUsageUnknown", messages.TiersTierUsageUnknownTiersUnusedTier)
		}
		for _, rule := range state.Config.Rules {
			if rule.Transition.StorageClass == name || rule.NoncurrentVersionTransition.StorageClass == name {
				return apperror.New(409, "TierInUse", messages.TiersTierInUse)
			}
		}
	}
	return nil
}

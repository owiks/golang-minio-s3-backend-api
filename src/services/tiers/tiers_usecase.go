package tiers

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/s3utils"

	"minio-web/api/src/services/apperror"
	endpoints "minio-web/api/src/services/endpoint"
	"minio-web/api/src/services/iam"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/result"
	validationsvc "minio-web/api/src/services/validation"
)

func ListTiers(ctx context.Context, s ListTiersClient) ([]TierView, error) {
	items, e := s.ListTiers(ctx)
	if e != nil {
		return nil, TierError(e)
	}
	out := []TierView{}
	for _, c := range items {
		if c != nil {
			out = append(out, TierSummary(c))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

type tierOperation uint8

const (
	createTier tierOperation = iota
	verifyTier
	updateTier
	removeTier
)

type CreateTierCommand struct{ Name, Type, Endpoint, Bucket, Prefix, Region, StorageClass, AccessKey, SecretKey string }
type VerifyTierCommand struct{ Name string }
type UpdateTierCredentialsCommand struct{ Name, Revision, Confirmation, AccessKey, SecretKey string }
type RemoveTierCommand struct{ Name, Revision, Confirmation string }

func CreateTier(ctx context.Context, allowed map[string]bool, s CreateTierClient, cmd CreateTierCommand) (result.Success, error) {
	return mutateTier(ctx, allowed, s, createTier, tierChange{Name: cmd.Name, Type: cmd.Type, Endpoint: cmd.Endpoint, Bucket: cmd.Bucket, Prefix: cmd.Prefix, Region: cmd.Region, StorageClass: cmd.StorageClass, AccessKey: cmd.AccessKey, SecretKey: cmd.SecretKey})
}
func VerifyTier(ctx context.Context, allowed map[string]bool, s CreateTierClient, cmd VerifyTierCommand) (result.Success, error) {
	return mutateTier(ctx, allowed, s, verifyTier, tierChange{Name: cmd.Name})
}
func UpdateTierCredentials(ctx context.Context, allowed map[string]bool, s CreateTierClient, cmd UpdateTierCredentialsCommand) (result.Success, error) {
	return mutateTier(ctx, allowed, s, updateTier, tierChange{Name: cmd.Name, Revision: cmd.Revision, Confirmation: cmd.Confirmation, AccessKey: cmd.AccessKey, SecretKey: cmd.SecretKey})
}
func RemoveTier(ctx context.Context, allowed map[string]bool, s CreateTierClient, cmd RemoveTierCommand) (result.Success, error) {
	return mutateTier(ctx, allowed, s, removeTier, tierChange{Name: cmd.Name, Revision: cmd.Revision, Confirmation: cmd.Confirmation})
}

// Private normalized state is constructed only by an operation-specific command.
type tierChange struct{ Name, Type, Endpoint, Bucket, Prefix, Region, StorageClass, AccessKey, SecretKey, Revision, Confirmation string }

func mutateTier(ctx context.Context, allowedEndpoints map[string]bool, s CreateTierClient, operation tierOperation, body tierChange) (result.Success, error) {
	if !validationsvc.ValidName(body.Name) || body.Name == "." || body.Name == ".." || (operation == createTier && !validationsvc.TierNamePattern.MatchString(body.Name)) {
		return result.Success{}, apperror.New(400, "InvalidTierName", messages.TiersInvalidTierName)
	}
	if e := s.RefreshActionPolicy(ctx); e != nil {
		return result.Success{}, apperror.New(403, "TierPermissionUnknown", messages.TiersTierPermissionUnknown)
	}
	policy := s.ActionPolicySnapshot()
	allowed := policy.Allows(s.ActorName(), iam.Query{Action: "admin:SetTier"})
	if !allowed {
		return result.Success{}, apperror.New(403, "AccessDenied", messages.TiersAccessDenied)
	}
	items, e := s.ListTiers(ctx)
	if e != nil {
		return result.Success{}, TierError(e)
	}
	var existing *madmin.TierConfig
	for _, c := range items {
		if c != nil && c.Name == body.Name {
			existing = c
			break
		}
	}
	if operation == verifyTier {
		if existing == nil {
			return result.Success{}, apperror.New(404, "TierNotFound", messages.TiersTierNotFound)
		}
		if e = s.VerifyTier(ctx, body.Name); e != nil {
			return result.Success{}, TierError(e)
		}
		return result.Success{OK: true}, nil
	}
	if operation == createTier {
		if existing != nil {
			return result.Success{}, apperror.New(409, "TierExists", messages.TiersTierExists)
		}
		if body.Type != "s3" && body.Type != "minio" {
			return result.Success{}, apperror.New(400, "InvalidTierType", messages.TiersInvalidTierType)
		}
		if s3utils.CheckValidBucketNameStrict(body.Bucket) != nil || len(body.Prefix) > 1024 || len(body.Region) > 128 || len(body.StorageClass) > 128 || strings.ContainsFunc(body.Prefix+body.Region+body.StorageClass, unicode.IsControl) {
			return result.Success{}, apperror.New(400, "InvalidTierTarget", messages.TiersInvalidTierTarget)
		}
		u, e := endpoints.Parse(body.Endpoint)
		if e != nil {
			return result.Success{}, e
		}
		// The remote server performs future connections itself. Require an explicit
		// allowlist entry: a one-time DNS check in this API cannot pin its future IPs.
		if !allowedEndpoints[u.String()] {
			return result.Success{}, apperror.New(400, "TierEndpointNotAllowed", messages.TiersTierEndpointNotAllowed)
		}
		if u.String() == s.PublicEndpoint().String() || u.String() == s.InternalEndpoint().String() {
			return result.Success{}, apperror.New(400, "TierSameServer", messages.TiersTierSameServer)
		}
		body.Endpoint = u.String()
	} else {
		if existing == nil {
			return result.Success{}, apperror.New(404, "TierNotFound", messages.TiersTierNotFound)
		}
		if body.Revision == "" || body.Revision != TierSummary(existing).Revision {
			return result.Success{}, apperror.New(409, "TierChanged", messages.TiersTierChanged)
		}
		if body.Confirmation != body.Name {
			return result.Success{}, apperror.New(400, "TierConfirmation", messages.TiersTierConfirmation)
		}
	}
	if operation == removeTier {
		if e = UnusedTier(ctx, s, body.Name); e != nil {
			return result.Success{}, e
		}
		if e = s.RemoveTier(ctx, body.Name); e != nil {
			return result.Success{}, TierError(e)
		}
	} else {
		if len(body.AccessKey) < 3 || len(body.AccessKey) > 256 || len(body.SecretKey) < 8 || len(body.SecretKey) > 1024 || strings.ContainsFunc(body.AccessKey+body.SecretKey, unicode.IsControl) {
			return result.Success{}, apperror.New(400, "InvalidTierCredentials", messages.TiersInvalidTierCredentials)
		}
		if operation == updateTier {
			if !TierSummary(existing).Editable {
				return result.Success{}, apperror.New(400, "TierCredentialsUnsupported", messages.TiersTierCredentialsUnsupported)
			}
			e = s.EditTier(ctx, body.Name, madmin.TierCreds{AccessKey: body.AccessKey, SecretKey: body.SecretKey})
		} else {
			var c *madmin.TierConfig
			if body.Type == "minio" {
				c, e = madmin.NewTierMinIO(body.Name, body.Endpoint, body.AccessKey, body.SecretKey, body.Bucket, madmin.MinIOPrefix(body.Prefix), madmin.MinIORegion(body.Region))
			} else {
				c, e = madmin.NewTierS3(body.Name, body.AccessKey, body.SecretKey, body.Bucket, madmin.S3Endpoint(body.Endpoint), madmin.S3Prefix(body.Prefix), madmin.S3Region(body.Region), madmin.S3StorageClass(body.StorageClass))
			}
			if e == nil {
				e = s.AddTier(ctx, c)
			}
		}
		if e != nil {
			return result.Success{}, TierError(e)
		}
	}
	return result.Success{OK: true}, nil
}

type TiersRequest struct {
	Operation    string `json:"operation"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Endpoint     string `json:"endpoint"`
	Bucket       string `json:"bucket"`
	Prefix       string `json:"prefix"`
	Region       string `json:"region"`
	StorageClass string `json:"storageClass"`
	AccessKey    string `json:"accessKey"`
	SecretKey    string `json:"secretKey"`
	Revision     string `json:"revision"`
	Confirmation string `json:"confirmation"`
}

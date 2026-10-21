package buckets

import (
	"context"
	"fmt"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/lifecycle"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

type LifecycleChangeRequest struct {
	Revision     string          `json:"revision"`
	Index        *int            `json:"index"`
	Rule         *LifecycleInput `json:"rule"`
	Enabled      *bool           `json:"enabled"`
	Confirmation string          `json:"confirmation"`
}

func ChangeLifecycle(ctx context.Context, s ChangeLifecycleClient, bucket string, operation string, body LifecycleChangeRequest) (DetailField, error) {
	state, err := ReadLifecycle(ctx, s, bucket)
	if err != nil {
		return DetailField{}, err
	}
	if body.Revision == "" || body.Revision != state.View.Revision {
		return DetailField{}, apperror.New(409, "BucketSettingChanged", messages.BucketsBucketSettingChangedBucketLifecycleChange)
	}
	if !state.View.Writable {
		return DetailField{}, apperror.New(409, "LifecycleUnsupported", state.View.Warning)
	}
	cfg := state.Config
	index := -1
	if operation != "create" {
		if body.Index == nil || *body.Index < 0 || *body.Index >= len(cfg.Rules) {
			return DetailField{}, apperror.New(404, "LifecycleRuleNotFound", messages.BucketsLifecycleRuleNotFound)
		}
		index = *body.Index
	}
	switch operation {
	case "create", "update":
		if body.Rule == nil {
			return DetailField{}, apperror.New(400, "InvalidLifecycleRule", messages.BucketsInvalidLifecycleRule)
		}
		if operation == "create" && len(cfg.Rules) >= 1000 {
			return DetailField{}, apperror.New(400, "LifecycleLimit", messages.BucketsLifecycleLimit)
		}
		old := lifecycle.Rule{}
		if index >= 0 {
			old = cfg.Rules[index]
		}
		updated, err := ApplyLifecycleInput(old, *body.Rule)
		if err != nil {
			return DetailField{}, err
		}
		for i, rule := range cfg.Rules {
			if i != index && rule.ID == updated.ID {
				return DetailField{}, apperror.New(409, "LifecycleDuplicateID", messages.BucketsCORSDuplicateID)
			}
		}
		if index < 0 {
			cfg.Rules = append(cfg.Rules, updated)
		} else {
			cfg.Rules[index] = updated
		}
	case "toggle":
		if body.Enabled == nil {
			return DetailField{}, apperror.New(400, "InvalidLifecycleRule", messages.BucketsInvalidLifecycleRuleBucketLifecycleChange)
		}
		cfg.Rules[index].Status = "Disabled"
		if *body.Enabled {
			cfg.Rules[index].Status = "Enabled"
		}
	case "delete":
		expected := cfg.Rules[index].ID
		if expected == "" {
			expected = fmt.Sprintf("Правило %d", index+1)
		}
		if body.Confirmation != expected {
			return DetailField{}, apperror.New(400, "ConfirmationRequired", messages.BucketsConfirmationRequiredBucketLifecycleChange)
		}
		cfg.Rules = append(cfg.Rules[:index], cfg.Rules[index+1:]...)
	default:
		return DetailField{}, apperror.New(405, "MethodNotAllowed", messages.BucketsMethodNotAllowed)
	}
	status, err := s.WriteLifecycleConfig(ctx, bucket, cfg)
	if err != nil {
		switch minio.ToErrorResponse(err).Code {
		case "InvalidArgument", "InvalidRequest", "InvalidLifecycleConfiguration", "MalformedXML":
			return DetailField{}, apperror.New(400, "InvalidLifecycleRule", messages.BucketsInvalidLifecycleRuleBucketLifecycleChangeChangeLifecycle)
		}
		return DetailField{}, err
	}
	// The SDK's empty-config DELETE does not itself validate HTTP status.
	if status < 200 || status >= 300 {
		if status == 403 {
			return DetailField{}, apperror.New(403, "AccessDenied", messages.BucketsAccessDenied)
		}
		return DetailField{}, apperror.New(502, "LifecycleWriteFailed", messages.BucketsLifecycleWriteFailed)
	}
	stored, readErr := ReadLifecycle(ctx, s, bucket)
	return LifecycleField(stored, readErr), nil
}

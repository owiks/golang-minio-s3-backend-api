package buckets

import (
	"context"
	"strings"
	"unicode"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/cors"
	"github.com/minio/minio-go/v7/pkg/sse"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

type BrowserChangeRequest struct {
	Revision     string    `json:"revision"`
	Index        *int      `json:"index"`
	Rule         *CorsRule `json:"rule"`
	Algorithm    string    `json:"algorithm"`
	KeyID        string    `json:"keyId"`
	Confirmation string    `json:"confirmation"`
}

func ChangeBrowserSetting(ctx context.Context, s ChangeBrowserSettingsClient, bucket, kind string, operation string, body BrowserChangeRequest) (DetailField, error) {
	state, err := ReadBrowserSetting(ctx, s, bucket, kind)
	if err != nil {
		return DetailField{}, err
	}
	if body.Revision == "" || body.Revision != state.Revision {
		return DetailField{}, apperror.New(409, "BucketSettingChanged", messages.BucketsBucketSettingChanged)
	}
	if !state.Value.Writable {
		return DetailField{}, apperror.New(409, "UnsupportedBucketSetting", state.Value.Warning)
	}
	var status int
	if kind == "cors" {
		rules := state.Cors.CORSRules
		index := -1
		if operation != "create" {
			if body.Index == nil || *body.Index < 0 || *body.Index >= len(rules) {
				return DetailField{}, apperror.New(404, "CORSRuleNotFound", messages.BucketsCORSRuleNotFound)
			}
			index = *body.Index
		}
		switch operation {
		case "create", "update":
			if body.Rule == nil {
				return DetailField{}, apperror.New(400, "InvalidCORS", messages.BucketsInvalidCORSBucketBrowserSettingsChange)
			}
			if err := ValidateCorsRule(*body.Rule); err != nil {
				return DetailField{}, err
			}
			if index < 0 && len(rules) >= 100 {
				return DetailField{}, apperror.New(400, "InvalidCORS", messages.BucketsInvalidCORSBucketBrowserSettingsChangeChangeBrowserSetting)
			}
			input := body.Rule
			for i, rule := range rules {
				if input.ID != "" && rule.ID == input.ID && i != index {
					return DetailField{}, apperror.New(409, "CORSDuplicateID", messages.BucketsCORSDuplicateID)
				}
			}
			parsed := cors.Rule{ID: input.ID, AllowedOrigin: input.Origins, AllowedMethod: input.Methods, AllowedHeader: input.Headers, ExposeHeader: input.Expose, MaxAgeSeconds: input.MaxAge}
			if index < 0 {
				rules = append(rules, parsed)
			} else {
				rules[index] = parsed
			}
		case "delete":
			if body.Confirmation != "delete" {
				return DetailField{}, apperror.New(400, "ConfirmationRequired", messages.BucketsConfirmationRequired)
			}
			rules = append(rules[:index], rules[index+1:]...)
		default:
			return DetailField{}, apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
		}
		var cfg *cors.Config
		if len(rules) > 0 {
			cfg = cors.NewConfig(rules)
		}
		status, err = s.WriteCORSConfig(ctx, bucket, cfg)
	} else {
		if operation != "update" {
			return DetailField{}, apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
		}
		if strings.ContainsFunc(body.KeyID, unicode.IsControl) || len(body.KeyID) > 1024 {
			return DetailField{}, apperror.New(400, "InvalidEncryption", messages.BucketsInvalidEncryption)
		}
		switch body.Algorithm {
		case "":
			if body.Confirmation != "remove-default" {
				return DetailField{}, apperror.New(400, "ConfirmationRequired", messages.BucketsConfirmationRequiredBucketBrowserSettingsChange)
			}
			status, err = s.WriteEncryptionConfig(ctx, bucket, nil)
		case "AES256":
			if body.KeyID != "" {
				return DetailField{}, apperror.New(400, "InvalidEncryption", messages.BucketsInvalidEncryptionBucketBrowserSettingsChange)
			}
			status, err = s.WriteEncryptionConfig(ctx, bucket, sse.NewConfigurationSSES3())
		case "aws:kms":
			status, err = s.WriteEncryptionConfig(ctx, bucket, sse.NewConfigurationSSEKMS(body.KeyID))
		default:
			return DetailField{}, apperror.New(400, "InvalidEncryption", messages.BucketsInvalidEncryptionBucketBrowserSettingsChangeChangeBrowserSetting)
		}
	}
	if err != nil {
		switch minio.ToErrorResponse(err).Code {
		case "KMSNotConfigured", "KMSKeyNotFound", "KMSAccessDeniedException", "kms:KeyNotFound", "kms:NotSupported":
			return DetailField{}, apperror.New(409, "KMSUnavailable", messages.BucketsKMSUnavailable)
		case "InvalidArgument", "MalformedXML":
			return DetailField{}, apperror.New(400, "InvalidBucketSetting", messages.BucketsInvalidBucketSetting)
		}
		return DetailField{}, err
	}
	if status < 200 || status >= 300 {
		if status == 403 {
			return DetailField{}, apperror.New(403, "AccessDenied", messages.ApperrorInsufficientMinIOPermissionsForThisOperation)
		}
		return DetailField{}, apperror.New(502, "BucketSettingWriteFailed", messages.BucketsBucketSettingWriteFailed)
	}
	stored, readErr := ReadBrowserSetting(ctx, s, bucket, kind)
	return BrowserSettingField(stored, readErr), nil
}

package objects

import (
	"context"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/encrypt"
	"github.com/minio/minio-go/v7/pkg/tags"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	objectwrite "minio-web/api/src/services/objects/write"
)

func GetProperties(ctx context.Context, s GetPropertiesClient, bucket, key string) (ObjectProperties, error) {
	bucket, key, err := ObjectParams(bucket, key)
	if err != nil {
		return ObjectProperties{}, err
	}
	if strings.HasSuffix(key, "/") {
		return ObjectProperties{}, apperror.New(400, "InvalidObject", messages.ObjectsInvalidObjectObjectPropertiesUsecase)
	}

	_, current, err := ReadProperties(ctx, s, bucket, key)
	if err != nil {
		return ObjectProperties{}, err
	}
	return current, nil
}

func UpdateProperties(ctx context.Context, s UpdatePropertiesClient, bucket, key string, body ObjectPropertiesRequest) (PropertiesUpdated, error) {
	bucket, key, err := ObjectParams(bucket, key)
	if err != nil {
		return PropertiesUpdated{}, err
	}
	if strings.HasSuffix(key, "/") {
		return PropertiesUpdated{}, apperror.New(400, "InvalidObject", messages.ObjectsInvalidObjectObjectPropertiesUsecase)
	}

	if err = ValidatePropertyPatch(body.Headers, body.Metadata, body.Tags, body.MetadataMode, body.TagsMode); err != nil {
		return PropertiesUpdated{}, err
	}

	info, current, err := ReadProperties(ctx, s, bucket, key)
	if err != nil {
		return PropertiesUpdated{}, err
	}

	if body.Revision == "" || body.Revision != current.Revision {
		return PropertiesUpdated{}, apperror.New(409, "ObjectPropertiesChanged", messages.ObjectsObjectPropertiesChanged)
	}
	if !messages.Empty(current.TagsError) {
		return PropertiesUpdated{}, apperror.New(403, "ObjectTagsUnavailable", current.TagsError)
	}
	metaChange := body.Headers != nil || body.Metadata != nil
	if !metaChange && body.Tags == nil {
		return PropertiesUpdated{}, apperror.New(400, "EmptyPropertyPatch", messages.ObjectsEmptyPropertyPatch)
	}
	if body.Headers != nil {
		for k, v := range body.Headers {
			current.Headers[k] = v
		}
	}
	if body.Metadata != nil {
		current.Metadata = MergeProperties(current.Metadata, body.Metadata, body.MetadataMode)
	}
	if body.Tags != nil {
		current.Tags = MergeProperties(current.Tags, body.Tags, body.TagsMode)
	}
	if err = ValidatePropertyPatch(current.Headers, current.Metadata, current.Tags, "replace", "replace"); err != nil {
		return PropertiesUpdated{}, err
	}
	if metaChange {
		if !messages.Empty(current.MetadataReason) {
			return PropertiesUpdated{}, apperror.New(409, "ObjectMetadataProtected", current.MetadataReason)
		}
		safeMetadata := map[string]string{}
		for k, v := range current.Metadata {
			safeMetadata["x-amz-meta-"+k] = v
		}
		opts := minio.PutObjectOptions{UserMetadata: safeMetadata, UserTags: current.Tags, ContentType: current.Headers["Content-Type"], CacheControl: current.Headers["Cache-Control"], ContentDisposition: current.Headers["Content-Disposition"], ContentEncoding: current.Headers["Content-Encoding"], ContentLanguage: current.Headers["Content-Language"], Expires: info.Expires, StorageClass: info.StorageClass, PartSize: 16 * 1024 * 1024, NumThreads: 1}
		if opts.ContentType == "" {
			opts.ContentType = "application/octet-stream"
		}
		if info.Metadata.Get("X-Amz-Server-Side-Encryption") == "AES256" {
			opts.ServerSideEncryption = encrypt.NewSSE()
		}
		// Conditional PUT/CompleteMultipartUpload protects the destination too.
		// A pinned old source version must never overwrite a newer body.
		if redirect := info.Metadata.Get("X-Amz-Website-Redirect-Location"); redirect != "" {
			opts.UserMetadata["x-amz-website-redirect-location"] = redirect
		}
		opts.SetMatchETag(info.ETag)
		err = RewriteObject(ctx, s, bucket, key, info, opts)
	} else {
		var value *tags.Tags
		value, err = tags.NewTags(current.Tags, true)
		if err == nil {
			err = s.PutObjectTagging(ctx, bucket, key, value, minio.PutObjectTaggingOptions{VersionID: info.VersionID})
			if code := minio.ToErrorResponse(err).Code; code == "NoSuchVersion" || code == "NoSuchKey" {
				err = propertyTargetChanged()
			}
			if err == nil && !immutablePropertyVersion(info.VersionID) {
				err = verifyTaggedTarget(ctx, s, bucket, key, info)
			}
		}
	}
	if err != nil {
		return PropertiesUpdated{}, objectwrite.FileWriteError(err)
	}
	return PropertiesUpdated{OK: true, MetadataRewritten: metaChange}, nil
}

type ObjectPropertiesRequest struct {
	Revision     string            `json:"revision"`
	Headers      map[string]string `json:"headers"`
	Metadata     map[string]string `json:"metadata"`
	Tags         map[string]string `json:"tags"`
	MetadataMode string            `json:"metadataMode"`
	TagsMode     string            `json:"tagsMode"`
}

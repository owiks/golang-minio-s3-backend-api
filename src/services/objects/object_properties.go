package objects

import (
	"context"
	"encoding/json"
	"mime"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/tags"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	objectstate "minio-web/api/src/services/objects/state"
	"minio-web/api/src/services/revision"
)

type ObjectProperties struct {
	Revision       string            `json:"revision"`
	Headers        map[string]string `json:"headers"`
	Metadata       map[string]string `json:"metadata"`
	Tags           map[string]string `json:"tags"`
	TagsError      messages.Ref      `json:"tagsError,omitempty"`
	MetadataReason messages.Ref      `json:"metadataReason,omitempty"`
	Version        string            `json:"version"`
}

func propertyHeaders() [5]string {
	return [5]string{"Content-Type", "Cache-Control", "Content-Disposition", "Content-Encoding", "Content-Language"}
}

func PropertiesRevision(info minio.ObjectInfo, p ObjectProperties) string {
	raw, _ := json.Marshal(struct {
		State                   objectstate.DeletionSnapshot
		Headers, Metadata, Tags map[string]string
		TagsError               string
	}{objectstate.DeletionSnapshot{ETag: info.ETag, Size: info.Size, Modified: info.LastModified, Version: info.VersionID}, p.Headers, p.Metadata, p.Tags, tagReadState(p.TagsError)})
	return revision.Of(raw)
}
func ReadProperties(ctx context.Context, s GetPropertiesClient, bucket, key string) (minio.ObjectInfo, ObjectProperties, error) {
	info, err := s.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	p := ObjectProperties{Headers: map[string]string{}, Metadata: map[string]string{}, Tags: map[string]string{}}
	if err != nil {
		return info, p, err
	}
	p.Version = info.VersionID
	for _, name := range propertyHeaders() {
		p.Headers[name] = info.Metadata.Get(name)
	}
	p.Headers["Content-Type"] = info.ContentType
	for k, v := range info.UserMetadata {
		p.Metadata[strings.ToLower(k)] = v
	}
	tagset, err := s.GetObjectTagging(ctx, bucket, key, minio.GetObjectTaggingOptions{VersionID: info.VersionID})
	if err != nil {
		p.TagsError = messages.ObjectsTagsAreUnavailableCheckPermissionToRead
	} else {
		p.Tags = tagset.ToMap()
		if !immutablePropertyVersion(info.VersionID) {
			if err := verifyPropertyTarget(ctx, s, bucket, key, info); err != nil {
				return info, ObjectProperties{}, err
			}
		}
	}
	if e := info.Metadata.Get("X-Amz-Server-Side-Encryption"); e != "" && e != "AES256" {
		p.MetadataReason = messages.ObjectsEditingMetadataForThisEncryptionTypeIs
	}
	if info.Metadata.Get("X-Amz-Object-Lock-Mode") != "" || info.Metadata.Get("X-Amz-Object-Lock-Legal-Hold") == "ON" {
		p.MetadataReason = messages.ObjectsMetadataOfAProtectedObjectIsRead
	}
	p.Revision = PropertiesRevision(info, p)
	return info, p, nil
}
func MergeProperties(old, patch map[string]string, mode string) map[string]string {
	out := map[string]string{}
	if mode != "replace" {
		for k, v := range old {
			out[k] = v
		}
	}
	for k, v := range patch {
		if mode == "remove" {
			delete(out, k)
		} else {
			out[k] = v
		}
	}
	return out
}
func ValidatePropertyPatch(headers, metadata, tagset map[string]string, metadataMode, tagsMode string) error {
	clean := func(s string) bool { return utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl) }
	for _, mode := range []string{metadataMode, tagsMode} {
		if mode != "" && mode != "merge" && mode != "replace" && mode != "remove" {
			return apperror.New(400, "InvalidPropertyMode", messages.ObjectsInvalidPropertyMode)
		}
	}
	for k, v := range headers {
		found := false
		for _, name := range propertyHeaders() {
			if name == k {
				found = true
			}
		}
		if !found || len(v) > 2048 || !clean(v) {
			return apperror.New(400, "InvalidObjectHeaders", messages.ObjectsInvalidObjectHeaders)
		}
		if k == "Content-Type" && v != "" {
			if media, _, err := mime.ParseMediaType(v); err != nil || !strings.Contains(media, "/") {
				return apperror.New(400, "InvalidObjectHeaders", messages.ObjectsInvalidObjectHeadersObjectProperties)
			}
		}
	}
	total := 0
	for k, v := range metadata {
		if k == "" || len(k) > 128 || !clean(k) || !clean(v) || k != strings.ToLower(k) || strings.HasPrefix(k, "x-amz-") {
			return apperror.New(400, "InvalidObjectMetadata", messages.ObjectsInvalidObjectMetadata)
		}
		for _, c := range k {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
				return apperror.New(400, "InvalidObjectMetadata", messages.ObjectsInvalidObjectMetadataObjectProperties)
			}
		}
		total += len(k) + len(v)
	}
	if total > 2048 {
		return apperror.New(400, "InvalidObjectMetadata", messages.ObjectsInvalidObjectMetadataObjectPropertiesValidatePropertyPatch)
	}
	if _, err := tags.NewTags(tagset, true); err != nil {
		return apperror.New(400, "InvalidObjectTags", messages.ObjectsInvalidObjectTags)
	}
	return nil
}

func tagReadState(problem messages.Ref) string {
	if messages.Empty(problem) {
		return ""
	}
	return "tags_unavailable"
}

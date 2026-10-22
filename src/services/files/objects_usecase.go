package files

import (
	"context"
	"io"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/minio/minio-go/v7"

	accesssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/access"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	objectssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
)

type ObjectListOptions struct {
	Bucket, Prefix, Token string
}

type ObjectLinkOptions struct {
	Mode      string
	ExpiresIn int
}

func ListBuckets(ctx context.Context, s ListBucketsClient) ([]objectssvc.BucketRow, error) {
	items, err := accesssvc.AccessibleBuckets(ctx, s)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func ListObjects(ctx context.Context, s ListObjectsClient, input ObjectListOptions) (objectssvc.ObjectPage, error) {
	bucket, prefix := input.Bucket, input.Prefix
	if bucket == "" || len(prefix) > 1024 || len(input.Token) > 8192 {
		return objectssvc.ObjectPage{}, apperror.New(400, "InvalidListing", messages.FilesInvalidListing)
	}
	page, err := s.ListObjectPage(ctx, bucket, prefix, input.Token, "/", 200)
	if err != nil {
		return objectssvc.ObjectPage{}, err
	}
	entries := make([]objectssvc.Entry, 0, len(page.CommonPrefixes)+len(page.Contents))
	for _, folder := range page.CommonPrefixes {
		entries = append(entries, objectssvc.Entry{Key: folder.Prefix, Name: strings.TrimSuffix(strings.TrimPrefix(folder.Prefix, prefix), "/"), Kind: "folder"})
	}
	for _, obj := range page.Contents {
		if obj.Key == prefix && obj.Size == 0 {
			continue
		}
		modified := obj.LastModified
		entries = append(entries, objectssvc.Entry{Key: obj.Key, Name: strings.TrimPrefix(obj.Key, prefix), Kind: "file", Size: obj.Size, Modified: &modified, ETag: obj.ETag})
	}
	objectssvc.FillFolderDates(ctx, s, bucket, entries)
	next := ""
	if page.IsTruncated {
		next = page.NextContinuationToken
	}
	return objectssvc.ObjectPage{Entries: entries, NextToken: next}, nil
}

func GetObjectInfo(ctx context.Context, s GetObjectInfoClient, bucket, key string) (ObjectInfo, error) {
	b, k, err := objectssvc.ObjectParams(bucket, key)
	if err != nil {
		return ObjectInfo{}, err
	}
	info, err := s.StatObject(ctx, b, k, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, err
	}
	metadata := info.UserMetadata
	if metadata == nil {
		metadata = map[string]string{}
	}
	return ObjectInfo{Size: info.Size, ContentType: info.ContentType, Modified: info.LastModified, ETag: info.ETag, VersionID: info.VersionID, Metadata: metadata}, nil
}

func GetPreview(ctx context.Context, s GetPreviewClient, bucket, key string) (objectssvc.PreviewData, error) {
	b, k, err := objectssvc.ObjectParams(bucket, key)
	if err != nil {
		return objectssvc.PreviewData{}, err
	}
	kind, mime := objectssvc.PreviewKind(k)
	if kind == "unsupported" || kind == "audio" || kind == "video" {
		return objectssvc.PreviewData{}, apperror.New(415, "UnsupportedPreview", messages.FilesUnsupportedPreview)
	}
	limit := int64(20 * 1024 * 1024)
	if kind == "text" {
		limit = 1024 * 1024
	}
	info, err := s.StatObject(ctx, b, k, minio.StatObjectOptions{})
	if err != nil {
		return objectssvc.PreviewData{}, err
	}
	if info.Size > limit {
		return objectssvc.PreviewData{}, apperror.New(413, "PreviewTooLarge", messages.FilesPreviewTooLarge)
	}
	// A range starting at byte zero is invalid for an empty S3 object.
	// Read the bounded object with an ETag condition instead; a concurrent
	// replacement must not silently change the preview after the HEAD.
	opts := minio.GetObjectOptions{}
	_ = opts.SetMatchETag(info.ETag)
	obj, err := s.GetObject(ctx, b, k, opts)
	if err != nil {
		return objectssvc.PreviewData{}, err
	}
	defer obj.Close()
	data, err := io.ReadAll(io.LimitReader(obj, limit+1))
	if err != nil {
		if code := minio.ToErrorResponse(err).Code; code == "PreconditionFailed" || code == "ConditionalRequestConflict" {
			return objectssvc.PreviewData{}, apperror.New(409, "ObjectChanged", messages.FilesObjectChanged)
		}
		return objectssvc.PreviewData{}, err
	}
	if int64(len(data)) > limit || int64(len(data)) != info.Size {
		return objectssvc.PreviewData{}, apperror.New(409, "ObjectChanged", messages.FilesObjectChangedObjectsUsecase)
	}
	return objectssvc.PreviewData{ContentType: mime, Data: data}, nil
}

func GetObjectLink(ctx context.Context, s GetObjectLinkClient, bucket, key string, input ObjectLinkOptions) (ObjectLink, error) {
	b, k, err := objectssvc.ObjectParams(bucket, key)
	if err != nil {
		return ObjectLink{}, err
	}
	mode := input.Mode
	expires := 60
	params := make(url.Values)
	if mode == "media" {
		kind, mime := objectssvc.PreviewKind(k)
		if kind != "audio" && kind != "video" {
			return ObjectLink{}, apperror.New(400, "UnsupportedMedia", messages.FilesUnsupportedMedia)
		}
		expires = 3600
		params.Set("response-content-type", mime)
		params.Set("response-content-disposition", "inline")
	} else {
		if mode == "share" {

			duration := input.ExpiresIn
			if duration < 1 || duration > 604800 {
				return ObjectLink{}, apperror.New(400, "InvalidLifetime", messages.FilesInvalidLifetime)
			}
			expires = duration
		} else if mode != "download" {
			return ObjectLink{}, apperror.New(400, "InvalidMode", messages.FilesInvalidMode)
		}
		name := path.Base(k)
		name = strings.Map(func(c rune) rune {
			if unicode.IsControl(c) {
				return '_'
			}
			return c
		}, name)
		if name == "." || name == "/" {
			name = "download"
		}
		encoded := strings.ReplaceAll(url.QueryEscape(name), "+", "%20")
		params.Set("response-content-disposition", `attachment; filename="download"; filename*=UTF-8''`+encoded)
		params.Set("response-content-type", "application/octet-stream")
	}
	signed, err := s.PresignedGetObject(ctx, b, k, time.Duration(expires)*time.Second, params)
	if err != nil {
		return ObjectLink{}, err
	}
	if signed == nil {
		return ObjectLink{}, apperror.New(502, "InvalidSignedURL", messages.InternalError)
	}
	date, dateErr := time.Parse("20060102T150405Z", signed.Query().Get("X-Amz-Date"))
	seconds, ttlErr := strconv.ParseInt(signed.Query().Get("X-Amz-Expires"), 10, 64)
	if dateErr != nil || ttlErr != nil || seconds < 1 || seconds > int64(expires) {
		return ObjectLink{}, apperror.New(502, "InvalidSignedURL", messages.InternalError)
	}
	return ObjectLink{URL: signed.String(), ExpiresAt: date.Add(time.Duration(seconds) * time.Second)}, nil
}

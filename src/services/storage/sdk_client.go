package storage

import (
	"context"

	"github.com/minio/minio-go/v7"
)

// S3Client keeps the high-level SDK methods and adapts the few Core operations
// used for bounded multipart work. Consumers depend on their own small interfaces.
type S3Client struct{ *minio.Client }

func NewS3Client(client *minio.Client) *S3Client { return &S3Client{Client: client} }

func (c *S3Client) NewMultipartUpload(ctx context.Context, bucket, key string, opts minio.PutObjectOptions) (string, error) {
	return (minio.Core{Client: c.Client}).NewMultipartUpload(ctx, bucket, key, opts)
}
func (c *S3Client) ListMultipartUploads(ctx context.Context, bucket, prefix, keyMarker, uploadIDMarker, delimiter string, maxUploads int) (minio.ListMultipartUploadsResult, error) {
	return (minio.Core{Client: c.Client}).ListMultipartUploads(ctx, bucket, prefix, keyMarker, uploadIDMarker, delimiter, maxUploads)
}
func (c *S3Client) ListObjectParts(ctx context.Context, bucket, key, uploadID string, partNumberMarker, maxParts int) (minio.ListObjectPartsResult, error) {
	return (minio.Core{Client: c.Client}).ListObjectParts(ctx, bucket, key, uploadID, partNumberMarker, maxParts)
}
func (c *S3Client) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	return (minio.Core{Client: c.Client}).AbortMultipartUpload(ctx, bucket, key, uploadID)
}
func (c *S3Client) CopyObjectPart(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey, uploadID string, partID int, offset, length int64, metadata map[string]string) (minio.CompletePart, error) {
	return (minio.Core{Client: c.Client}).CopyObjectPart(ctx, srcBucket, srcKey, dstBucket, dstKey, uploadID, partID, offset, length, metadata)
}
func (c *S3Client) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []minio.CompletePart, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	return (minio.Core{Client: c.Client}).CompleteMultipartUpload(ctx, bucket, key, uploadID, parts, opts)
}

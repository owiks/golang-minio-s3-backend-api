package objects

import (
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/encrypt"
)

const MaxTextContent int64 = 1024 * 1024

func PreservedContentOptions(info minio.ObjectInfo, p ObjectProperties) minio.PutObjectOptions {
	metadata := map[string]string{}
	for k, v := range p.Metadata {
		metadata["x-amz-meta-"+k] = v
	}
	if redirect := info.Metadata.Get("X-Amz-Website-Redirect-Location"); redirect != "" {
		metadata["x-amz-website-redirect-location"] = redirect
	}
	opts := minio.PutObjectOptions{UserMetadata: metadata, UserTags: p.Tags, ContentType: p.Headers["Content-Type"], CacheControl: p.Headers["Cache-Control"], ContentDisposition: p.Headers["Content-Disposition"], ContentEncoding: p.Headers["Content-Encoding"], ContentLanguage: p.Headers["Content-Language"], Expires: info.Expires, StorageClass: info.StorageClass, DisableMultipart: true}
	if info.Metadata.Get("X-Amz-Server-Side-Encryption") == "AES256" {
		opts.ServerSideEncryption = encrypt.NewSSE()
	}
	opts.SetMatchETag(info.ETag)
	return opts
}

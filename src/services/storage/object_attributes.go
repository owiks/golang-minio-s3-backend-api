package storage

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

// ObjectAttributesOptions is intentionally smaller than the SDK's options: the
// API accepts neither arbitrary headers nor caller-selected encryption keys.
type ObjectAttributesOptions struct {
	VersionID        string
	MaxParts         int
	PartNumberMarker int
}

type ObjectAttributePart struct {
	Number    int               `json:"number"`
	Size      int64             `json:"size"`
	Checksums map[string]string `json:"checksums,omitempty"`
}

// Pointer fields distinguish an omitted upstream attribute from a reported zero.
// This is a detached result, never a live SDK response or shared cache value.
type ObjectAttributesPage struct {
	VersionID            string                `json:"versionId,omitempty"`
	ETag                 string                `json:"etag,omitempty"`
	Size                 *int64                `json:"size,omitempty"`
	StorageClass         string                `json:"storageClass,omitempty"`
	Checksums            map[string]string     `json:"checksums,omitempty"`
	ChecksumType         string                `json:"checksumType,omitempty"`
	PartsCount           *int                  `json:"partsCount,omitempty"`
	Parts                []ObjectAttributePart `json:"parts"`
	Truncated            *bool                 `json:"truncated,omitempty"`
	NextPartNumberMarker *int                  `json:"nextPartNumberMarker,omitempty"`
	LastModified         time.Time             `json:"lastModified"`
}

type attributesCaptureKey struct{}
type attributesCapture struct {
	mu       sync.Mutex
	document *attributesDocument
	options  ObjectAttributesOptions
}

// ReadObjectAttributes uses the official SDK; the checked transport records
// field presence before SDK XML decoding can collapse absent values into zeros.
func (c *S3Client) ReadObjectAttributes(ctx context.Context, bucket, key string, opts ObjectAttributesOptions) (ObjectAttributesPage, error) {
	if opts.MaxParts < 1 || opts.MaxParts > 1000 || opts.PartNumberMarker < 0 || opts.PartNumberMarker > 10000 {
		return ObjectAttributesPage{}, apperror.New(400, "InvalidObjectAttributes", messages.StorageInvalidObjectAttributes)
	}
	capture := &attributesCapture{options: opts}
	ctx = context.WithValue(ctx, attributesCaptureKey{}, capture)
	value, err := c.Client.GetObjectAttributes(ctx, bucket, key, minio.ObjectAttributesOptions{VersionID: opts.VersionID, MaxParts: opts.MaxParts, PartNumberMarker: opts.PartNumberMarker})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchVersion" {
			return ObjectAttributesPage{}, apperror.NewWithCause(404, "NoSuchVersion", messages.StorageNoSuchVersion, err)
		}
		// This SDK emits an untyped error for a server that ignores ?attributes and
		// returns an ordinary object with an ETag response header.
		if err.Error() == "getObjectAttributes is not supported by the current endpoint version" {
			return ObjectAttributesPage{}, apperror.New(501, "ObjectAttributesUnsupported", messages.StorageObjectAttributesUnsupported)
		}
		return ObjectAttributesPage{}, err
	}
	capture.mu.Lock()
	doc := capture.document
	capture.mu.Unlock()
	if value == nil || doc == nil {
		return ObjectAttributesPage{}, invalidObjectAttributes()
	}
	// Do not accept an unpinned response to a concrete version request. "null"
	// deliberately remains mutable and is not treated as an immutable identity.
	if opts.VersionID != "" && value.VersionID != opts.VersionID {
		return ObjectAttributesPage{}, invalidObjectAttributes()
	}
	result := ObjectAttributesPage{VersionID: value.VersionID, LastModified: value.LastModified}
	if doc.etag {
		result.ETag = strings.Trim(value.ETag, "\"")
	}
	if doc.size {
		n := int64(value.ObjectSize)
		result.Size = &n
	}
	if doc.storageClass {
		result.StorageClass = value.StorageClass
	}
	result.Checksums = value.ChecksumMap()
	if value.Checksum.ChecksumType == "FULL_OBJECT" || value.Checksum.ChecksumType == "COMPOSITE" {
		result.ChecksumType = value.Checksum.ChecksumType
	}
	if doc.partsCount {
		n := value.ObjectParts.PartsCount
		result.PartsCount = &n
	}
	if doc.objectParts {
		result.Parts = []ObjectAttributePart{}
	}
	if len(doc.parts) != len(value.ObjectParts.Parts) {
		return ObjectAttributesPage{}, invalidObjectAttributes()
	}
	for _, part := range value.ObjectParts.Parts {
		if part == nil {
			return ObjectAttributesPage{}, invalidObjectAttributes()
		}
		result.Parts = append(result.Parts, ObjectAttributePart{Number: part.PartNumber, Size: int64(part.Size), Checksums: partChecksums(part)})
	}
	if doc.truncated {
		v := value.ObjectParts.IsTruncated
		result.Truncated = &v
	}
	if value.ObjectParts.IsTruncated {
		n := value.ObjectParts.NextPartNumberMarker
		result.NextPartNumberMarker = &n
	}
	return result, nil
}

func partChecksums(part *minio.ObjectAttributePart) map[string]string {
	response := minio.ObjectAttributesResponse{}
	response.Checksum.ChecksumCRC32 = part.ChecksumCRC32
	response.Checksum.ChecksumCRC32C = part.ChecksumCRC32C
	response.Checksum.ChecksumCRC64NVME = part.ChecksumCRC64NVME
	response.Checksum.ChecksumSHA1 = part.ChecksumSHA1
	response.Checksum.ChecksumSHA256 = part.ChecksumSHA256
	response.Checksum.ChecksumMD5 = part.ChecksumMD5
	response.Checksum.ChecksumSHA512 = part.ChecksumSHA512
	response.Checksum.ChecksumXXHash64 = part.ChecksumXXHash64
	response.Checksum.ChecksumXXHash3 = part.ChecksumXXHash3
	response.Checksum.ChecksumXXHash128 = part.ChecksumXXHash128
	return response.ChecksumMap()
}

func invalidObjectAttributes() error {
	return apperror.New(502, "InvalidObjectAttributesResponse", messages.StorageInvalidObjectAttributesResponse)
}

// IsInvalidObjectAttributes identifies our validation failure without exposing
// a raw upstream body to diagnostics or the client.
func IsInvalidObjectAttributes(err error) bool {
	var own *apperror.Error
	return errors.As(err, &own) && own.Code == "InvalidObjectAttributesResponse"
}

package uploads

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/revision"
)

type UploadParts struct {
	Size         int64        `json:"size"`
	Count        int          `json:"count"`
	Revision     string       `json:"revision"`
	LastModified time.Time    `json:"lastModified"`
	Items        []UploadPart `json:"items,omitempty"`
}

type UploadPart struct {
	Number       int       `json:"number"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"lastModified"`
}

func InspectParts(ctx context.Context, s InspectIncompleteUploadClient, bucket, key, id string) (UploadParts, error) {
	core := s
	parts := make([]minio.ObjectPart, 0)
	marker := 0
	var size int64
	last := 0
	invalid := func() (UploadParts, error) {
		return UploadParts{}, apperror.New(502, "InvalidUploadPartsResponse", "S3 вернул некорректный список частей. Обновите сведения перед повтором операции.")
	}
	for page := 0; page < 10; page++ {
		result, err := core.ListObjectParts(ctx, bucket, key, id, marker, 1000)
		if err != nil {
			return UploadParts{}, err
		}

		if len(result.ObjectParts) > 1000 || len(parts)+len(result.ObjectParts) > 10000 {
			return invalid()
		}
		for _, part := range result.ObjectParts {
			if part.PartNumber <= last || part.PartNumber <= marker || part.PartNumber > 10000 || part.Size < 0 || part.Size > math.MaxInt64-size {
				return invalid()
			}
			last = part.PartNumber
			size += part.Size
		}
		if result.IsTruncated && (len(result.ObjectParts) == 0 || result.NextPartNumberMarker != last || last <= marker || last >= 10000) {
			return invalid()
		}

		parts = append(parts, result.ObjectParts...)
		if !result.IsTruncated {
			raw, _ := json.Marshal(parts)
			details := make([]UploadPart, 0, len(parts))
			var modified time.Time
			for _, part := range parts {
				details = append(details, UploadPart{Number: part.PartNumber, Size: part.Size, LastModified: part.LastModified})
				if part.LastModified.After(modified) {
					modified = part.LastModified
				}
			}
			return UploadParts{Size: size, Count: len(parts), Revision: revision.Of(raw), LastModified: modified, Items: details}, nil
		}
		marker = result.NextPartNumberMarker
	}
	return UploadParts{}, apperror.New(409, "PartsLimit", "Не удалось получить полный список частей. Обновите сведения.")
}

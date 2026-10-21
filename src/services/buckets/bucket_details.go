package buckets

import (
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

type UsageRow struct {
	Size          uint64 `json:"size"`
	Objects       uint64 `json:"objects"`
	Versions      uint64 `json:"versions"`
	DeleteMarkers uint64 `json:"deleteMarkers"`
}
type UsageReply struct {
	UpdatedAt *time.Time          `json:"updatedAt,omitempty"`
	Buckets   map[string]UsageRow `json:"buckets"`
}

func UsageRows(info madmin.DataUsageInfo, names []minio.BucketInfo) UsageReply {
	result := UsageReply{Buckets: map[string]UsageRow{}}
	if info.LastUpdate.IsZero() {
		return result
	}
	result.UpdatedAt = &info.LastUpdate
	for _, bucket := range names {
		if item, ok := info.BucketsUsage[bucket.Name]; ok {
			result.Buckets[bucket.Name] = UsageRow{Size: item.Size, Objects: item.ObjectsCount, Versions: item.VersionsCount, DeleteMarkers: item.DeleteMarkersCount}
		}
	}
	return result
}

type DetailField struct {
	Value     any          `json:"value,omitempty"`
	Error     messages.Ref `json:"error,omitempty"`
	Revision  string       `json:"revision,omitempty"`
	ErrorCode string       `json:"errorCode,omitempty"`
}

func Detail(value any, err error) DetailField {
	if err != nil {
		issue := apperror.Public(err)
		return DetailField{Error: issue.Message, ErrorCode: issue.Code}
	}
	return DetailField{Value: value}
}

package buckets

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"

	accesssvc "minio-web/api/src/services/access"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

func ReadUsage(ctx context.Context, s ReadUsageClient) (UsageReply, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	rows, err := accesssvc.AccessibleBuckets(ctx, s)
	if err != nil {
		return UsageReply{}, err
	}
	info, err := s.DataUsageInfo(ctx)
	if err != nil {
		return UsageReply{}, err
	}
	names := make([]minio.BucketInfo, 0, len(rows))
	for _, b := range rows {
		names = append(names, minio.BucketInfo{Name: b.Name, CreationDate: b.Created})
	}
	return UsageRows(info, names), nil
}
func ReadDetails(ctx context.Context, s ReadDetailsClient, bucket, rawFields string) (*BucketDetails, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	// Each field read authorizes itself; one denied field does not hide others.
	allowed := map[string]bool{"policy": true, "quota": true, "versioning": true, "locking": true, "tags": true, "lifecycle": true, "cors": true, "encryption": true}
	fields := []string{"policy", "quota", "versioning", "locking", "tags", "lifecycle", "cors", "encryption"}
	if raw := rawFields; raw != "" {
		if len(raw) > 128 {
			return nil, apperror.New(400, "InvalidBucketField", messages.BucketsInvalidBucketField)
		}
		fields = strings.Split(raw, ",")
	}
	for _, field := range fields {
		if !allowed[field] {
			return nil, apperror.New(400, "InvalidBucketField", messages.BucketsInvalidBucketField)
		}
	}
	result := &BucketDetails{Name: bucket}
	var jobs sync.WaitGroup
	var lock sync.Mutex
	seen := map[string]bool{}
	for _, field := range fields {
		if seen[field] {
			continue
		}
		seen[field] = true
		jobs.Go(func() {
			var value DetailField
			switch field {
			case "lifecycle":
				v, e := ReadLifecycle(ctx, s, bucket)
				value = LifecycleField(v, e)
			case "cors", "encryption":
				v, e := ReadBrowserSetting(ctx, s, bucket, field)
				value = BrowserSettingField(v, e)
			default:
				v, e := ReadBucketSetting(ctx, s, bucket, field)
				value = SettingField(v, e)
			}
			lock.Lock()
			switch field {
			case "policy":
				result.Policy = &value
			case "quota":
				result.Quota = &value
			case "tags":
				result.Tags = &value
			case "versioning":
				result.Versioning = &value
			case "locking":
				result.Locking = &value
			case "lifecycle":
				result.Lifecycle = &value
			case "cors":
				result.CORS = &value
			case "encryption":
				result.Encryption = &value
			}
			lock.Unlock()
		})
	}
	jobs.Wait()
	return result, nil
}

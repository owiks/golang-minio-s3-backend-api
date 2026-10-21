package server

import (
	"context"
	"time"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/messages"
)

func ReadServerInfo(ctx context.Context, s ReadServerInfoClient) (ServerSnapshot, error) {
	value, err := s.ServerInfo(ctx, madmin.WithDriveMetrics(true))
	if err != nil {
		return ServerSnapshot{}, err
	}
	// Whitelist UI fields; never forward environment, licenses or service configuration.
	result := ServerRows(value)
	// Optional scanner statistics need a separate permission. A failure must not hide nodes/drives.
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	usage, err := s.DataUsageInfo(ctx)
	if err == nil && !usage.LastUpdate.IsZero() {
		result.Usage = &ScannerUsage{UpdatedAt: usage.LastUpdate, Buckets: usage.BucketsCount, Objects: usage.ObjectsTotalCount, Size: usage.ObjectsTotalSize}
	} else {
		result.UsageError = messages.ServerObjectStatisticsAreUnavailableScannerDataOr
	}
	return result, nil
}

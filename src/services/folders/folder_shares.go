package folders

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
)

func SharePrefix(prefix string) bool {
	return len(prefix) <= 1024 && utf8.ValidString(prefix) && (prefix == "" || strings.HasSuffix(prefix, "/"))
}

func ScopedObjectPage(ctx context.Context, s FolderDeleteItemsClient, bucket, prefix, token, delimiter string, maxKeys int) (minio.ListBucketV2Result, error) {
	return s.ListObjectPage(ctx, bucket, prefix, token, delimiter, maxKeys)
}

func ParseShareRange(raw string, size int64) (int64, int64, error) {
	bad := func() (int64, int64, error) {
		return 0, 0, apperror.New(416, "InvalidRange", "Недоступный диапазон файла.")
	}
	if size <= 0 || !strings.HasPrefix(raw, "bytes=") || strings.Contains(raw, ",") {
		return bad()
	}
	left, right, ok := strings.Cut(strings.TrimPrefix(raw, "bytes="), "-")
	if !ok {
		return bad()
	}
	if left == "" {
		n, e := strconv.ParseInt(right, 10, 64)
		if e != nil || n <= 0 {
			return bad()
		}
		return max(0, size-n), size - 1, nil
	}
	start, e := strconv.ParseInt(left, 10, 64)
	if e != nil || start < 0 || start >= size {
		return bad()
	}
	end := size - 1
	if right != "" {
		end, e = strconv.ParseInt(right, 10, 64)
		if e != nil || end < start {
			return bad()
		}
		end = min(end, size-1)
	}
	return start, end, nil
}

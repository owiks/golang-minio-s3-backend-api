package folders

import (
	"context"
	"github.com/minio/minio-go/v7"
	"minio-web/api/src/services/apperror"
	"testing"
)

type cyclicFolderListing struct{ calls int }

func (s *cyclicFolderListing) ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error) {
	s.calls++
	return minio.ListBucketV2Result{Contents: []minio.ObjectInfo{{Key: "folder/file", Size: 4}}, IsTruncated: true, NextContinuationToken: []string{"A", "B", "A"}[min(s.calls-1, 2)]}, nil
}
func TestFolderInspectionRejectsCursorCycleEvenWithDuplicateObjects(t *testing.T) {
	source := &cyclicFolderListing{}
	items, err := FolderDeleteItems(context.Background(), source, "test-bucket", "folder/")
	if err == nil || apperror.Public(err).Code != "InvalidFolderListing" || items != nil || source.calls != 3 {
		t.Fatal("cyclic listing accepted", items, err, source.calls)
	}
}

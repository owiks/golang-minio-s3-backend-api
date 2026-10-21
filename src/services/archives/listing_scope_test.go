package archives

import (
	"context"
	"github.com/minio/minio-go/v7"
	"minio-web/api/src/services/apperror"
	"testing"
)

type malformedArchiveSource struct {
	archiveSource
	cycle bool
	calls int
}

func (s *malformedArchiveSource) ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error) {
	s.calls++
	if !s.cycle {
		return minio.ListBucketV2Result{Contents: []minio.ObjectInfo{{Key: "another-folder/private", Size: 4}}}, nil
	}
	next := []string{"A", "B", "A"}[min(s.calls-1, 2)]
	return minio.ListBucketV2Result{IsTruncated: true, NextContinuationToken: next}, nil
}
func TestArchiveCannotPublishEscapedOrCyclicListing(t *testing.T) {
	for _, cycle := range []bool{false, true} {
		for _, modern := range []bool{false, true} {
			source := &malformedArchiveSource{archiveSource: archiveSource{t: t}, cycle: cycle}
			store := &archivePublisher{}
			prefix := "folder/"
			body := PrepareArchiveRequest{Bucket: "test-bucket", Prefix: &prefix}
			if modern {
				body.Name = "download.zip"
			}
			_, err := PrepareArchive(context.Background(), store, source, body)
			if err == nil || apperror.Public(err).Code != "InvalidObjectListing" || store.plan != nil || source.calls > 3 {
				t.Fatal("unsafe archive published", modern, cycle, err, source.calls)
			}
		}
	}
}

package search

import (
	"context"
	"errors"
	"github.com/minio/minio-go/v7"
	"testing"
)

func TestListingGuardRejectsEscapesCyclesAndUnboundedEmptyPages(t *testing.T) {
	for _, tc := range []struct {
		name string
		page minio.ListBucketV2Result
	}{
		{"prefix-escape", minio.ListBucketV2Result{Contents: []minio.ObjectInfo{{Key: "other/file"}}}},
		{"negative-size", minio.ListBucketV2Result{Contents: []minio.ObjectInfo{{Key: "folder/file", Size: -1}}}},
		{"missing-cursor", minio.ListBucketV2Result{IsTruncated: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if NewListingGuard("folder/", 4).Check(context.Background(), "", tc.page) == nil {
				t.Fatal("invalid listing accepted")
			}
		})
	}
	guard := NewListingGuard("folder/", 4)
	for i, step := range []struct{ token, next string }{{"", "A"}, {"A", "B"}, {"B", "A"}} {
		err := guard.Check(context.Background(), step.token, minio.ListBucketV2Result{IsTruncated: true, NextContinuationToken: step.next})
		if (err != nil) != (i == 2) {
			t.Fatalf("cycle step %d: %v", i, err)
		}
	}
	guard = NewListingGuard("folder/", 2)
	if err := guard.Check(context.Background(), "", minio.ListBucketV2Result{IsTruncated: true, NextContinuationToken: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := guard.Check(context.Background(), "A", minio.ListBucketV2Result{IsTruncated: true, NextContinuationToken: "B"}); err == nil {
		t.Fatal("empty pages bypassed bound")
	}
}
func TestListingGuardPreservesOpaqueKeysAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewListingGuard("folder/", 2).Check(ctx, "", minio.ListBucketV2Result{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := NewListingGuard("folder/../", 2).Check(context.Background(), "", minio.ListBucketV2Result{Contents: []minio.ObjectInfo{{Key: "folder/../file"}}}); err != nil {
		t.Fatal("S3 key was normalized", err)
	}
	if err := NewListingGuard("folder/", 2).Check(context.Background(), "", minio.ListBucketV2Result{}); err != nil {
		t.Fatal("empty folder rejected", err)
	}
}

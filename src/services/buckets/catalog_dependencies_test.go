package buckets

import (
	"context"
	"errors"
	"testing"

	"github.com/minio/minio-go/v7"
	"minio-web/api/src/services/apperror"
)

type bucketCreator struct {
	t       *testing.T
	exists  bool
	failure error
	made    bool
	locked  *bool
}

func (f *bucketCreator) BucketExists(context.Context, string) (bool, error) {
	if !*f.locked {
		f.t.Fatal("existence check ran outside the mutation lock")
	}
	return f.exists, nil
}
func (f *bucketCreator) RegionName() string { return "fixture-region" }
func (f *bucketCreator) MakeBucket(_ context.Context, name string, opts minio.MakeBucketOptions) error {
	if !*f.locked || name != "test-bucket" || opts.Region != "fixture-region" || !opts.ObjectLocking {
		f.t.Fatal("creation lost lock or options")
	}
	f.made = true
	return f.failure
}
func TestCreateBucketUsesBoundMutationLock(t *testing.T) {
	for _, outcome := range []string{"created", "upstream error", "already exists", "lock error"} {
		t.Run(outcome, func(t *testing.T) {
			locked, released := false, 0
			upstream := errors.New("fixture error")
			client := &bucketCreator{t: t, locked: &locked, exists: outcome == "already exists"}
			if outcome == "upstream error" {
				client.failure = upstream
			}
			lock := func(ctx context.Context, resource string) (func(), error) {
				if resource != "bucket/test-bucket" || ctx.Err() != nil {
					t.Fatal("wrong lock scope")
				}
				if outcome == "lock error" {
					return nil, upstream
				}
				locked = true
				return func() { locked = false; released++ }, nil
			}
			err := CreateBucket(context.Background(), lock, client, CreateBucketRequest{Name: "test-bucket", ObjectLock: true})
			switch outcome {
			case "created":
				if err != nil || !client.made {
					t.Fatal(err)
				}
			case "upstream error":
				if !errors.Is(err, upstream) || !client.made {
					t.Fatal(err)
				}
			case "already exists":
				if apperror.Public(err).Code != "AlreadyExists" || client.made {
					t.Fatal(err)
				}
			case "lock error":
				if !errors.Is(err, upstream) || client.made || released != 0 {
					t.Fatal(err)
				}
				return
			}
			if locked || released != 1 {
				t.Fatal("mutation lock not released exactly once")
			}
		})
	}
}

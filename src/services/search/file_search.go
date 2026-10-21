package search

import (
	"context"
	"path"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"minio-web/api/src/services/apperror"
)

func RecursivePage(ctx context.Context, s RecursivePageClient, bucket, prefix, token string) (minio.ListBucketV2Result, error) {
	return s.ListObjectPage(ctx, bucket, prefix, token, "", 500)
}

type FileFilter struct {
	Text          string          `json:"-"`
	Extensions    map[string]bool `json:"-"`
	Min, Max      int64           `json:"-"`
	After, Before time.Time       `json:"-"`
}

func (f FileFilter) Matches(obj minio.ObjectInfo) bool {
	if f.Text != "" && !strings.Contains(strings.ToLower(obj.Key), f.Text) {
		return false
	}
	if len(f.Extensions) > 0 && !f.Extensions[strings.TrimPrefix(strings.ToLower(path.Ext(obj.Key)), ".")] {
		return false
	}
	return obj.Size >= f.Min && (f.Max < 0 || obj.Size <= f.Max) && (f.After.IsZero() || !obj.LastModified.Before(f.After)) && (f.Before.IsZero() || obj.LastModified.Before(f.Before))
}

// ListingGuard verifies one finite recursive traversal without normalizing S3 keys.
// The object budget is enforced by each consumer; page bounds also cover empty pages.
type ListingGuard struct {
	prefix          string
	pages, maxPages int
	seen            map[string]bool
}

func NewListingGuard(prefix string, maxPages int) *ListingGuard {
	return &ListingGuard{prefix: prefix, maxPages: maxPages, seen: map[string]bool{}}
}
func (g *ListingGuard) Check(ctx context.Context, token string, page minio.ListBucketV2Result) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	invalid := func() error {
		return apperror.New(502, "InvalidObjectListing", "S3 вернул некорректный или неполный список объектов. Повторите проверку.")
	}
	g.pages++
	if g.maxPages < 1 || g.pages > g.maxPages || g.seen[token] {
		return invalid()
	}
	g.seen[token] = true
	for _, object := range page.Contents {
		if !strings.HasPrefix(object.Key, g.prefix) || object.Size < 0 {
			return invalid()
		}
	}
	if page.IsTruncated && (page.NextContinuationToken == "" || g.seen[page.NextContinuationToken] || g.pages == g.maxPages) {
		return invalid()
	}
	return nil
}

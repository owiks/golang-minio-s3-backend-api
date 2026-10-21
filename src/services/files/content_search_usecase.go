package files

import (
	"context"
	"path"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	folderssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/folders"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	objectssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
	searchsvc "github.com/owiks/golang-minio-s3-backend-api/src/services/search"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
)

func SearchContent(ctx context.Context, s SearchContentClient, body searchsvc.ContentSearchBody) (ContentSearchResult, error) {
	if body.Bucket == "" || len(body.Bucket) > 63 || len(body.Prefix) > 1024 || len(body.Token) > 8192 || len(body.Query) == 0 || len(body.Query) > 256 || !utf8.ValidString(body.Query) || strings.ContainsAny(body.Query, "\r\n\x00") || len(body.Extensions) > 160 || body.MaxSize < 1 || body.MaxSize > objectssvc.MaxTextContent || body.PageSize < 1 || body.PageSize > 20 {
		return ContentSearchResult{}, apperror.New(400, "InvalidContentSearch", messages.FilesInvalidContentSearch)
	}
	extensions := searchsvc.NormalizeExtensions(body.Extensions)
	for ext := range extensions {
		kind, _ := objectssvc.PreviewKind("file." + ext)
		if kind != "text" {
			return ContentSearchResult{}, apperror.New(400, "InvalidContentExtensions", messages.FilesInvalidContentExtensions)
		}
	}
	page, err := folderssvc.ScopedObjectPage(ctx, s, body.Bucket, body.Prefix, body.Token, "", body.PageSize)
	if err != nil {
		return ContentSearchResult{}, err
	}
	type result struct {
		Match   *searchsvc.ContentMatch `json:"-"`
		Issue   *searchsvc.ContentIssue `json:"-"`
		Skipped bool                    `json:"-"`
		Read    bool                    `json:"-"`
	}
	results := make([]result, len(page.Contents))
	jobs := make(chan int, len(page.Contents))
	for i := range page.Contents {
		jobs <- i
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(2, len(page.Contents)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				obj := page.Contents[i]
				kind, _ := objectssvc.PreviewKind(obj.Key)
				ext := strings.TrimPrefix(strings.ToLower(path.Ext(obj.Key)), ".")
				if strings.HasSuffix(obj.Key, "/") || kind != "text" || len(extensions) > 0 && !extensions[ext] || obj.Size > body.MaxSize {
					results[i].Skipped = true
					continue
				}
				text, e := searchsvc.ReadSearchText(ctx, s, body.Bucket, obj, body.MaxSize)
				if e != nil {
					p := apperror.Public(e)
					results[i].Issue = &searchsvc.ContentIssue{Key: obj.Key, Code: p.Code, Message: p.Message}
					continue
				}
				results[i].Read = true
				hits, truncated := searchsvc.FindTextHits(text, body.Query, body.CaseSensitive)
				if len(hits) > 0 {
					results[i].Match = &searchsvc.ContentMatch{Key: obj.Key, ETag: strings.Trim(obj.ETag, "\""), Size: obj.Size, Hits: hits, Truncated: truncated}
				}
			}
		}()
	}
	workers.Wait()
	if ctx.Err() != nil {
		return ContentSearchResult{}, ctx.Err()
	}
	matches := []searchsvc.ContentMatch{}
	issues := []searchsvc.ContentIssue{}
	read, skipped := 0, 0
	for _, result := range results {
		if result.Match != nil {
			matches = append(matches, *result.Match)
		}
		if result.Issue != nil {
			issues = append(issues, *result.Issue)
		}
		if result.Read {
			read++
		}
		if result.Skipped {
			skipped++
		}
	}
	next := ""
	if page.IsTruncated {
		next = page.NextContinuationToken
	}
	if len(issues) > 0 {
		telemetry.Outcome(ctx, "partial", "ContentSearchPartial")
	}
	return ContentSearchResult{Matches: matches, Issues: issues, Scanned: len(page.Contents), Read: read, Skipped: skipped, NextToken: next}, nil
}

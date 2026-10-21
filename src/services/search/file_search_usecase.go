package search

import (
	"context"
	"path"
	"strings"

	objectssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
)

func SearchFiles(ctx context.Context, s RecursivePageClient, input FileSearchInput) (FileSearchResult, error) {
	f, err := prepareFileSearch(input)
	if err != nil {
		return FileSearchResult{}, err
	}
	page, err := RecursivePage(ctx, s, input.Bucket, input.Prefix, input.Token)
	if err != nil {
		return FileSearchResult{}, err
	}
	rows := make([]objectssvc.Entry, 0)
	for _, obj := range page.Contents {
		if strings.HasSuffix(obj.Key, "/") && obj.Size == 0 {
			continue
		}
		if f.Matches(obj) {
			modified := obj.LastModified
			rows = append(rows, objectssvc.Entry{Key: obj.Key, Name: path.Base(obj.Key), Kind: "file", Size: obj.Size, Modified: &modified, ETag: obj.ETag})
		}
	}
	next := ""
	if page.IsTruncated {
		next = page.NextContinuationToken
	}
	return FileSearchResult{Entries: rows, Scanned: len(page.Contents), NextToken: next}, nil
}

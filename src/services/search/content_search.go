package search

import (
	"context"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

type TextHit struct {
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Before string `json:"before"`
	Match  string `json:"match"`
	After  string `json:"after"`
}
type ContentMatch struct {
	Key       string    `json:"key"`
	ETag      string    `json:"etag"`
	Size      int64     `json:"size"`
	Hits      []TextHit `json:"hits"`
	Truncated bool      `json:"truncated"`
}
type ContentIssue struct {
	Key     string       `json:"key"`
	Code    string       `json:"code"`
	Message messages.Ref `json:"message"`
}
type ContentSearchBody struct {
	Bucket        string `json:"bucket"`
	Prefix        string `json:"prefix"`
	Query         string `json:"query"`
	Extensions    string `json:"extensions"`
	Token         string `json:"token"`
	CaseSensitive bool   `json:"caseSensitive"`
	MaxSize       int64  `json:"maxSize"`
	PageSize      int    `json:"pageSize"`
}

func FindTextHits(text, query string, sensitive bool) ([]TextHit, bool) {
	hits := []TextHit{}
	needle := query
	if !sensitive {
		needle = strings.ToLower(needle)
	}
	lineNumber := 0
	for remaining := text; ; {
		line, rest, hasNewline := strings.Cut(remaining, "\n")
		remaining = rest
		lineNumber++
		line = strings.TrimSuffix(line, "\r")
		if lineNumber == 1 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		comparison := line
		if !sensitive {
			comparison = strings.ToLower(comparison)
		}
		index := strings.Index(comparison, needle)
		if index >= 0 {
			if len(hits) >= 20 {
				return hits, true
			}
			start := utf8.RuneCountInString(comparison[:index])
			length := utf8.RuneCountInString(needle)
			runes := []rune(line)
			left, right := max(0, start-55), min(len(runes), start+length+90)
			before, after := string(runes[left:start]), string(runes[start+length:right])
			if left > 0 {
				before = "…" + before
			}
			if right < len(runes) {
				after += "…"
			}
			hits = append(hits, TextHit{Line: lineNumber, Column: start + 1, Before: before, Match: string(runes[start : start+length]), After: after})
		}
		if !hasNewline {
			break
		}
	}
	return hits, false
}
func ReadSearchText(ctx context.Context, s ReadSearchTextClient, bucket string, obj minio.ObjectInfo, maxSize int64) (string, error) {
	opts := minio.GetObjectOptions{}
	_ = opts.SetMatchETag(obj.ETag)
	object, err := s.GetObject(ctx, bucket, obj.Key, opts)
	if err != nil {
		return "", err
	}
	defer object.Close()
	info, err := object.Stat()
	if err != nil {
		return "", err
	}
	if info.Metadata.Get("Content-Encoding") != "" {
		return "", apperror.New(415, "EncodedText", messages.SearchEncodedText)
	}
	data, err := io.ReadAll(io.LimitReader(object, maxSize+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxSize {
		return "", apperror.New(413, "SearchFileTooLarge", messages.SearchSearchFileTooLarge)
	}
	if int64(len(data)) != obj.Size {
		return "", apperror.New(409, "SearchObjectChanged", messages.SearchSearchObjectChanged)
	}
	if !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
		return "", apperror.New(415, "NotUTF8Text", messages.FoldersInvalidText)
	}
	return string(data), nil
}

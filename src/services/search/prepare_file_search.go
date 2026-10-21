package search

import (
	"strconv"
	"strings"
	"time"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

// FileSearchInput contains decoded values, preserving empty optional fields and
// opaque S3 prefixes/cursors. Length limits apply before Unicode normalization.
type FileSearchInput struct {
	Bucket, Prefix, Token string
	Query, Extensions     string
	MinSize, MaxSize      string
	After, Before         string
}

func prepareFileSearch(input FileSearchInput) (FileFilter, error) {
	if input.Bucket == "" || len(input.Bucket) > 63 || len(input.Prefix) > 1024 || len(input.Token) > 8192 || len(input.Query) > 256 || len(input.Extensions) > 128 {
		return FileFilter{}, apperror.New(400, "InvalidSearch", messages.SearchInvalidSearch)
	}
	f := FileFilter{Text: strings.ToLower(input.Query), Extensions: NormalizeExtensions(input.Extensions), Max: -1}
	for _, value := range []struct {
		raw    string
		target *int64
	}{{input.MinSize, &f.Min}, {input.MaxSize, &f.Max}} {
		if value.raw != "" {
			n, err := strconv.ParseInt(value.raw, 10, 64)
			if err != nil || n < 0 {
				return FileFilter{}, apperror.New(400, "InvalidSearch", messages.SearchInvalidSearchPrepareFileSearch)
			}
			*value.target = n
		}
	}
	if f.Max >= 0 && f.Max < f.Min {
		return FileFilter{}, apperror.New(400, "InvalidSearch", messages.SearchInvalidSearchPrepareFileSearchFileSearch)
	}
	for _, value := range []struct {
		raw          string
		target       *time.Time
		inclusiveEnd bool
	}{{input.After, &f.After, false}, {input.Before, &f.Before, true}} {
		if value.raw != "" {
			date, err := time.Parse("2006-01-02", value.raw)
			if err != nil {
				return FileFilter{}, apperror.New(400, "InvalidSearch", messages.SearchInvalidSearchPrepareFileSearchFileSearch8EDB05)
			}
			if value.inclusiveEnd {
				date = date.AddDate(0, 0, 1)
			}
			*value.target = date
		}
	}
	if !f.After.IsZero() && !f.Before.IsZero() && !f.After.Before(f.Before) {
		return FileFilter{}, apperror.New(400, "InvalidSearch", messages.SearchInvalidSearchPrepareFileSearchFileSearch448466)
	}
	return f, nil
}

// NormalizeExtensions returns a new set for each request. A single leading dot
// is optional; empty entries are ignored and duplicate extensions are collapsed.
func NormalizeExtensions(raw string) map[string]bool {
	extensions := map[string]bool{}
	for _, ext := range strings.Split(strings.ToLower(raw), ",") {
		if ext = strings.TrimPrefix(strings.TrimSpace(ext), "."); ext != "" {
			extensions[ext] = true
		}
	}
	return extensions
}

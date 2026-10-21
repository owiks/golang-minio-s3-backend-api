package search

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testmessage"
)

func TestInvalidSearchIsRejectedBeforeStorage(t *testing.T) {
	const params = "Проверьте параметры поиска."
	const size = "Размер должен быть целым числом байт от нуля."
	for _, tc := range []struct {
		name    string
		input   FileSearchInput
		message string
	}{
		{"bucket-missing", FileSearchInput{}, params},
		{"bucket-limit", FileSearchInput{Bucket: strings.Repeat("b", 64)}, params},
		{"prefix-limit", FileSearchInput{Bucket: "bucket", Prefix: strings.Repeat("x", 1025)}, params},
		{"cursor-limit", FileSearchInput{Bucket: "bucket", Token: strings.Repeat("x", 8193)}, params},
		{"query-limit", FileSearchInput{Bucket: "bucket", Query: strings.Repeat("x", 257)}, params},
		{"extensions-limit", FileSearchInput{Bucket: "bucket", Extensions: strings.Repeat("x", 129)}, params},
		{"min-invalid", FileSearchInput{Bucket: "bucket", MinSize: "abc"}, size},
		{"min-negative", FileSearchInput{Bucket: "bucket", MinSize: "-1"}, size},
		{"max-negative", FileSearchInput{Bucket: "bucket", MaxSize: "-1"}, size},
		{"max-fraction", FileSearchInput{Bucket: "bucket", MaxSize: "1.5"}, size},
		{"size-overflow", FileSearchInput{Bucket: "bucket", MaxSize: "9223372036854775808"}, size},
		{"size-whitespace", FileSearchInput{Bucket: "bucket", MinSize: " 1"}, size},
		{"reversed-size", FileSearchInput{Bucket: "bucket", MinSize: "5", MaxSize: "4"}, "Максимальный размер меньше минимального."},
		{"bad-start", FileSearchInput{Bucket: "bucket", After: "2026-02-30"}, "Укажите дату в формате ГГГГ-ММ-ДД."},
		{"bad-end", FileSearchInput{Bucket: "bucket", Before: "2026-10-20T00:00:00Z"}, "Укажите дату в формате ГГГГ-ММ-ДД."},
		{"reversed-dates", FileSearchInput{Bucket: "bucket", After: "2026-10-20", Before: "2026-10-19"}, "Дата окончания раньше даты начала."},
		{"scope-before-parsing", FileSearchInput{MinSize: "abc", After: "wrong"}, params},
		{"size-before-date", FileSearchInput{Bucket: "bucket", MinSize: "5", MaxSize: "4", After: "wrong"}, "Максимальный размер меньше минимального."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := SearchFiles(context.Background(), nil, tc.input)
			if err == nil {
				t.Fatal("invalid input accepted")
			}
			public := apperror.Public(err)
			if public.Status != 400 || public.Code != "InvalidSearch" || testmessage.Russian(public.Message) != tc.message {
				t.Fatal("validation contract changed", public)
			}
		})
	}
}

func TestSearchPreparationDefaultsAndRawUnicodeLimits(t *testing.T) {
	f, err := prepareFileSearch(FileSearchInput{Bucket: strings.Repeat("b", 63), Prefix: strings.Repeat("p", 1024), Token: strings.Repeat("t", 8192)})
	if err != nil || f.Min != 0 || f.Max != -1 || f.Text != "" || len(f.Extensions) != 0 || !f.After.IsZero() || !f.Before.IsZero() {
		t.Fatal("default filter changed", err)
	}
	f, err = prepareFileSearch(FileSearchInput{Bucket: "bucket", Query: strings.Repeat("Ⱥ", 128), Extensions: strings.Repeat("Ⱥ", 64), MinSize: "+0001", MaxSize: "9223372036854775807"})
	if err != nil || len(f.Text) != 384 || !f.Extensions[strings.Repeat("ⱥ", 64)] || f.Min != 1 || f.Max != math.MaxInt64 {
		t.Fatal("raw byte limits, case folding or integer precision changed", err)
	}
	zero, err := prepareFileSearch(FileSearchInput{Bucket: "bucket", MaxSize: "0"})
	if err != nil || !zero.Matches(minio.ObjectInfo{Key: "empty", Size: 0}) || zero.Matches(minio.ObjectInfo{Key: "not-empty", Size: 1}) {
		t.Fatal("explicit zero confused with absent maximum", err)
	}
	spaces, err := prepareFileSearch(FileSearchInput{Bucket: "bucket", Query: " ФАЙЛ "})
	if err != nil || !spaces.Matches(minio.ObjectInfo{Key: "dir/ файл .txt"}) || spaces.Matches(minio.ObjectInfo{Key: "файл.txt"}) {
		t.Fatal("query whitespace was trimmed", err)
	}
}

func TestNormalizedExtensionsAreSharedRulesWithIndependentSets(t *testing.T) {
	raw := " .JSON, yaml, .json, , . , ..TXT "
	one, two := NormalizeExtensions(raw), NormalizeExtensions(raw)
	want := map[string]bool{"json": true, "yaml": true, ".txt": true}
	if !reflect.DeepEqual(one, want) || !reflect.DeepEqual(two, want) {
		t.Fatal("extension normalization changed", one)
	}
	delete(one, "json")
	one["exe"] = true
	if !reflect.DeepEqual(two, want) {
		t.Fatal("requests share an extension map")
	}
}

func TestSearchSizeAndInclusiveDateBoundaries(t *testing.T) {
	f, err := prepareFileSearch(FileSearchInput{Bucket: "bucket", Query: "ФАЙЛ", Extensions: " .JSON, .json ", MinSize: "4", MaxSize: "4", After: "2026-10-20", Before: "2026-10-20"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		modified string
		size     int64
		match    bool
	}{
		{"2026-10-19T23:59:59.999999999Z", 4, false},
		{"2026-10-20T00:00:00Z", 4, true},
		{"2026-10-20T23:59:59.999999999Z", 4, true},
		{"2026-10-21T00:00:00Z", 4, false},
		{"2026-10-20T03:00:00+03:00", 4, true},
		{"2026-10-21T03:00:00+03:00", 4, false},
		{"2026-10-20T12:00:00Z", 3, false},
		{"2026-10-20T12:00:00Z", 5, false},
	} {
		t.Run(tc.modified+"-"+string(rune('0'+tc.size)), func(t *testing.T) {
			modified, err := time.Parse(time.RFC3339Nano, tc.modified)
			if err != nil {
				t.Fatal(err)
			}
			if f.Matches(minio.ObjectInfo{Key: "dir/файл.JSON", Size: tc.size, LastModified: modified}) != tc.match {
				t.Fatal("filter boundary changed")
			}
		})
	}
}

type searchPageClient struct {
	page                             minio.ListBucketV2Result
	err                              error
	calls                            int
	ctx                              context.Context
	bucket, prefix, token, delimiter string
	limit                            int
}

func (s *searchPageClient) ListObjectPage(ctx context.Context, bucket, prefix, token, delimiter string, limit int) (minio.ListBucketV2Result, error) {
	s.calls++
	s.ctx = ctx
	s.bucket = bucket
	s.prefix = prefix
	s.token = token
	s.delimiter = delimiter
	s.limit = limit
	if err := ctx.Err(); err != nil {
		return minio.ListBucketV2Result{}, err
	}
	return s.page, s.err
}

func TestSearchPreservesNativeScopeCursorAndResultShape(t *testing.T) {
	modified := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	client := &searchPageClient{page: minio.ListBucketV2Result{IsTruncated: true, NextContinuationToken: "native+/=next", Contents: []minio.ObjectInfo{
		{Key: "folder/", Size: 0}, {Key: "folder/ФАЙЛ.JSON", Size: 4, LastModified: modified, ETag: "etag"}, {Key: "folder/файл.txt", Size: 4, LastModified: modified}, {Key: "folder/другое.JSON", Size: 4, LastModified: modified},
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := FileSearchInput{Bucket: "bucket", Prefix: "../%2F// папка/", Token: "opaque+/=cursor", Query: "файл", Extensions: " .JSON, json ", MinSize: "4", MaxSize: "4", After: "2026-10-20", Before: "2026-10-20"}
	result, err := SearchFiles(ctx, client, input)
	if err != nil || client.calls != 1 || client.ctx != ctx || client.bucket != input.Bucket || client.prefix != input.Prefix || client.token != input.Token || client.delimiter != "" || client.limit != 500 {
		t.Fatal("native request scope or page size changed", err)
	}
	if result.Scanned != 4 || result.NextToken != "native+/=next" || len(result.Entries) != 1 {
		t.Fatal("pagination or filtered results changed", result)
	}
	entry := result.Entries[0]
	if entry.Key != "folder/ФАЙЛ.JSON" || entry.Name != "ФАЙЛ.JSON" || entry.Kind != "file" || entry.Size != 4 || entry.ETag != "etag" || entry.Modified == nil || !entry.Modified.Equal(modified) {
		t.Fatal("entry projection changed", entry)
	}
	input.Query = "not-found"
	result, err = SearchFiles(ctx, client, input)
	if err != nil || result.Entries == nil || len(result.Entries) != 0 || result.Scanned != 4 || result.NextToken == "" {
		t.Fatal("empty filtered page lost its cursor", err)
	}
	raw, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(raw), `"entries":[]`) {
		t.Fatal("empty result became null", err)
	}
}

func TestSearchPreservesErrorsAndCancellation(t *testing.T) {
	for _, failure := range []error{errors.New("upstream"), minio.ErrorResponse{Code: "AccessDenied"}, context.Canceled} {
		t.Run(failure.Error(), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if errors.Is(failure, context.Canceled) {
				cancel()
			}
			client := &searchPageClient{err: failure, page: minio.ListBucketV2Result{IsTruncated: true, NextContinuationToken: "must-not-escape"}}
			result, err := SearchFiles(ctx, client, FileSearchInput{Bucket: "bucket"})
			if !errors.Is(err, failure) || client.calls != 1 || client.ctx != ctx || result.Entries != nil || result.NextToken != "" {
				t.Fatal("failed listing returned a partial page or retried", err)
			}
		})
	}
}

package integration

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	searchsvc "minio-web/api/src/services/search"
)

func TestSearchFilterDatesAndZeroSizeIntegration(t *testing.T) {
	c, s3, bucket := fileFixture(t)
	ctx := context.Background()
	const key = "dates/Отчёт.JSON"
	for name, body := range map[string]string{key: "data", "dates/empty.txt": "", "dates/": "", "other/Отчёт.JSON": "data"} {
		if _, err := s3.PutObject(ctx, bucket, name, strings.NewReader(body), int64(len(body)), minio.PutObjectOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	info, err := s3.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	day := info.LastModified.UTC()
	query := url.Values{
		"bucket": {bucket}, "prefix": {"dates/"}, "q": {"ОТЧЁТ"},
		"extensions": {" .JSON, json "}, "minSize": {"4"}, "maxSize": {"4"},
		"after": {day.Format("2006-01-02")}, "before": {day.Format("2006-01-02")},
	}
	read := func(wantKey string) {
		t.Helper()
		var result searchsvc.FileSearchResult
		if err := json.Unmarshal(c.call("GET", "objects/search?"+query.Encode(), nil, 200), &result); err != nil {
			t.Fatal(err)
		}
		if result.Scanned != 3 || result.NextToken != "" || result.Entries == nil {
			t.Fatal("scope, count or empty result contract changed", result)
		}
		if wantKey == "" {
			if len(result.Entries) != 0 {
				t.Fatal("date boundary admitted object", result)
			}
		} else if len(result.Entries) != 1 || result.Entries[0].Key != wantKey {
			t.Fatal("filter result changed", result)
		}
	}
	read(key)
	query.Del("after")
	query.Set("before", day.AddDate(0, 0, -1).Format("2006-01-02"))
	read("")
	query.Del("before")
	query.Set("after", day.AddDate(0, 0, 1).Format("2006-01-02"))
	read("")
	query.Del("after")
	query.Del("q")
	query.Del("extensions")
	query.Del("minSize")
	query.Set("maxSize", "0")
	read("dates/empty.txt")
}

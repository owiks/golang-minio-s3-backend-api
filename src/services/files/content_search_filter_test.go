package files

import (
	"context"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	searchsvc "github.com/owiks/golang-minio-s3-backend-api/src/services/search"
)

type contentFilterClient struct{ calls int }

func (s *contentFilterClient) ListObjectPage(context.Context, string, string, string, string, int) (minio.ListBucketV2Result, error) {
	s.calls++
	return minio.ListBucketV2Result{}, nil
}
func (*contentFilterClient) GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error) {
	panic("empty page must not open objects")
}

func TestContentSearchRetainsOwnLimitsAndExtensionRestrictions(t *testing.T) {
	for _, tc := range []struct{ extensions, query, code string }{
		{" .JSON, yaml, .TXT, .json ", "needle", ""},
		{" . , , ", "needle", ""},
		{strings.Repeat(" ", 150) + "txt", "needle", ""},
		{strings.Repeat(" ", 161), "needle", "InvalidContentSearch"},
		{"txt,.PNG", "needle", "InvalidContentExtensions"},
		{"exe", "needle", "InvalidContentExtensions"},
		{"txt", "", "InvalidContentSearch"},
		{"txt", "\xff", "InvalidContentSearch"},
		{"txt", "a\nb", "InvalidContentSearch"},
	} {
		t.Run(tc.extensions+"-"+tc.code, func(t *testing.T) {
			client := &contentFilterClient{}
			_, err := SearchContent(context.Background(), client, searchsvc.ContentSearchBody{Bucket: "bucket", Extensions: tc.extensions, Query: tc.query, MaxSize: 1024, PageSize: 2})
			if tc.code != "" {
				if err == nil || apperror.Public(err).Code != tc.code || client.calls != 0 {
					t.Fatal("invalid content filter reached storage", err, client.calls)
				}
			} else if err != nil || client.calls != 1 {
				t.Fatal("normalized content extensions rejected", err, client.calls)
			}
		})
	}
}

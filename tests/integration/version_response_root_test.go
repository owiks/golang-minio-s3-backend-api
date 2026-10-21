package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/minio/minio-go/v7"

	apihttp "minio-web/api/src/http/api"
	httpcommon "minio-web/api/src/http/api/common"
	"minio-web/api/src/http/presentation"
	objectssvc "minio-web/api/src/services/objects"
	runtimesvc "minio-web/api/src/services/runtime"
)

func TestVersionPaginationNativeIntegration(t *testing.T) {
	c, client, bucket := fileFixture(t)
	ctx := context.Background()
	if err := client.SetBucketVersioning(ctx, bucket, minio.BucketVersioningConfiguration{Status: "Enabled"}); err != nil {
		t.Fatal(err)
	}
	key := "folder/a +%.txt"
	expected := map[string]bool{}
	for i := range 52 {
		body := fmt.Sprintf("version-%d", i)
		info, err := client.PutObject(ctx, bucket, key, strings.NewReader(body), int64(len(body)), minio.PutObjectOptions{})
		if err != nil || info.VersionID == "" {
			t.Fatal("native version not created", info, err)
		}
		expected[info.VersionID] = true
	}
	if err := client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.PutObject(ctx, bucket, key+"-neighbor", strings.NewReader("other"), 5, minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	cursor, markers, pages := "", 0, 0
	for {
		query := url.Values{"bucket": {bucket}, "key": {key}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		var page objectssvc.VersionsPage
		if err := json.Unmarshal(c.call("GET", "admin/object-versions?"+query.Encode(), nil, 200), &page); err != nil {
			t.Fatal(err)
		}
		pages++
		for _, row := range page.Rows {
			if row.ID == "" || seen[row.ID] {
				t.Fatal("empty or duplicate native version", row.ID)
			}
			seen[row.ID] = true
			if row.Deleted {
				markers++
			} else if !expected[row.ID] {
				t.Fatal("neighbor or unexpected version leaked", row.ID)
			}
		}
		if page.Next == "" {
			break
		}
		if page.Next == cursor || pages >= 3 {
			t.Fatal("native continuation did not advance")
		}
		cursor = page.Next
	}
	if pages != 2 || len(seen) != 53 || markers != 1 {
		t.Fatal("incomplete native pagination", pages, len(seen), markers)
	}
}

func TestVersionListingHTTPRejectsErrorRootWithoutEmptySuccess(t *testing.T) {
	for _, tc := range []struct {
		name, body, code     string
		upstream, downstream int
	}{
		{"error in HTTP 200", `<Error><Code>AccessDenied</Code><Message>PRIVATE-UPSTREAM</Message></Error>`, "VersionListingFailed", 200, 502},
		{"namespaced error in HTTP 200", `<s3:Error xmlns:s3="http://s3.amazonaws.com/doc/2006-03-01/"><Code>InternalError</Code><Message>PRIVATE-UPSTREAM</Message></s3:Error>`, "VersionListingFailed", 200, 502},
		{"proxy document", `<html><body>PRIVATE-UPSTREAM</body></html>`, "VersionListingFailed", 200, 502},
		{"wrong listing", `<ListBucketResult/>`, "VersionListingFailed", 200, 502},
		{"second XML document", `<ListVersionsResult/><Error><Message>PRIVATE-UPSTREAM</Message></Error>`, "VersionListingFailed", 200, 502},
		{"trailing text", `<ListVersionsResult/>PRIVATE-UPSTREAM`, "VersionListingFailed", 200, 502},
		{"trailing malformed XML", `<ListVersionsResult/><`, "VersionListingFailed", 200, 502},
		{"missing continuation", `<ListVersionsResult><Version><Key>file</Key><VersionId>v1</VersionId></Version><IsTruncated>true</IsTruncated></ListVersionsResult>`, "VersionListingFailed", 200, 502},
		{"missing version marker", `<ListVersionsResult><Version><Key>file</Key><VersionId>v1</VersionId></Version><IsTruncated>true</IsTruncated><NextKeyMarker>file</NextKeyMarker></ListVersionsResult>`, "VersionListingFailed", 200, 502},
		{"empty truncated listing", `<ListVersionsResult><IsTruncated>true</IsTruncated><NextKeyMarker>file</NextKeyMarker><NextVersionIdMarker>v1</NextVersionIdMarker></ListVersionsResult>`, "VersionListingFailed", 200, 502},
		{"native denied", `<Error><Code>AccessDenied</Code><Message>PRIVATE-UPSTREAM</Message></Error>`, "AccessDenied", 403, 403},
		{"missing bucket", `<Error><Code>NoSuchBucket</Code><Message>PRIVATE-UPSTREAM</Message></Error>`, "NoSuchBucket", 404, 404},
		{"wrong error root", `<ListVersionsResult/>`, "VersionListingFailed", 403, 502},
		{"empty native listing", `<ListVersionsResult/>`, "", 200, 200},
		{"empty namespaced listing", `<ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IsTruncated>false</IsTruncated></ListVersionsResult>`, "", 200, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var listings atomic.Int32
			s := actionCheckSession(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/accountinfo") {
					httpcommon.WriteJSON(w, r, 200, map[string]any{"AccountName": "fixture-user", "Policy": actionCheckPolicy})
					return
				}
				if r.Method != "GET" || r.URL.Path != "/fixture-bucket/" || !r.URL.Query().Has("versions") || r.URL.Query().Get("prefix") != "file" {
					t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(400)
					return
				}
				listings.Add(1)
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(tc.upstream)
				_, _ = w.Write([]byte(tc.body))
			})
			app := runtimesvc.New(testConfig())
			defer app.Close()
			if err := app.StoreSession(s); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("GET", "http://ui.example/api/sessions/"+s.SessionID()+"/admin/object-versions?bucket=fixture-bucket&key=file", nil)
			r.Header.Set("Origin", "http://ui.example")
			r.AddCookie(httpcommon.SessionCookie(s, r, false))
			w := httptest.NewRecorder()
			apihttp.NewHandler(app).ServeHTTP(w, r)
			if w.Code != tc.downstream || strings.Contains(w.Body.String(), "PRIVATE") || listings.Load() != 1 {
				t.Fatal("root error hidden, exposed or retried", w.Code, w.Body.String(), listings.Load())
			}
			if tc.code != "" {
				var failure struct{ Error presentation.Error }
				if err := json.Unmarshal(w.Body.Bytes(), &failure); err != nil || failure.Error.Code != tc.code || strings.Contains(w.Body.String(), `"rows"`) {
					t.Fatal("wrong error envelope or false successful page", w.Body.String(), err)
				}
				return
			}
			var page objectssvc.VersionsPage
			if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Rows == nil || len(page.Rows) != 0 || page.Next != "" {
				t.Fatal("valid empty listing changed", w.Body.String(), err)
			}
		})
	}
}

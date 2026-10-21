package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	folders "minio-web/api/src/http/api/routes/folders/controllers"
	objects "minio-web/api/src/http/api/routes/objects/controllers"
	search "minio-web/api/src/http/api/routes/search/controllers"
	"minio-web/api/src/services/apperror"
	objecttypes "minio-web/api/src/services/objects"
	"minio-web/api/tests/testsession"
)

func TestSearchQueryLimitPrecedesUnicodeCaseFolding(t *testing.T) {
	query := strings.Repeat("Ⱥ", 100) // 200 input bytes, 300 after case folding.
	key := strings.ToLower(query) + ".txt"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<ListBucketResult><Name>test-data</Name><EncodingType>url</EncodingType><IsTruncated>false</IsTruncated><Contents><Key>%s</Key><Size>0</Size></Contents></ListBucketResult>`, url.QueryEscape(key))
	}))
	defer upstream.Close()
	endpoint, _ := url.Parse(upstream.URL)
	session := testsession.New(t, endpoint.String())
	response := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/?"+url.Values{"bucket": {"test-data"}, "q": {query}}.Encode(), nil)
	if err := search.HandleFileSearch(nil, response, r, session); err != nil {
		t.Fatal(err)
	}
	var result struct{ Entries []objecttypes.Entry }
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || len(result.Entries) != 1 || result.Entries[0].Key != key {
		t.Fatalf("case-folded match lost: %s, %v", response.Body.String(), err)
	}
}

func TestHTTPMethodsRejectedBeforeCallingServices(t *testing.T) {
	for name, path := range map[string]string{
		"objects": "objects", "content": "objects/content", "properties": "objects/properties", "deletion": "admin/object-delete", "usage": "objects/usage", "versions": "admin/object-versions", "search": "objects/search", "content-search": "objects/content-search", "create-folder": "objects/folders", "folder-delete": "objects/folder-delete", "folder-shares": "objects/folder-shares", "upload-invites": "objects/invites", "incomplete-uploads": "admin/incomplete-uploads", "zip": "objects/zip", "gzip": "objects/gzip", "prepare-archive": "objects/archive", "archive-status": "objects/archive-status/ticket", "tiers": "admin/tiers",
	} {
		t.Run(name, func(t *testing.T) {
			err := runHTTPAction(nil, httptest.NewRecorder(), httptest.NewRequest("PATCH", "/?bucket=test-data&key=file.txt&prefix=folder/", nil), nil, path)
			if err == nil || apperror.Public(err).Status != http.StatusMethodNotAllowed {
				t.Fatalf("expected 405 before storage, got %v", err)
			}
		})
	}
}

func TestSearchDecodingRejectsInvalidValuesBeforeStorage(t *testing.T) {
	for _, query := range []string{"minSize=abc", "minSize=-1", "maxSize=9223372036854775808", "minSize=5&maxSize=4", "after=2026-02-30", "after=2026-10-20&before=2026-10-19"} {
		t.Run(query, func(t *testing.T) {
			err := search.HandleFileSearch(nil, httptest.NewRecorder(), httptest.NewRequest("GET", "/?bucket=test-data&"+query, nil), nil)
			if err == nil || apperror.Public(err).Code != "InvalidSearch" {
				t.Fatalf("expected InvalidSearch, got %v", err)
			}
		})
	}
}

func TestDeleteRejectsVersionParameterEvenWhenEmpty(t *testing.T) {
	for _, parameter := range []string{"version", "versionId"} {
		r := httptest.NewRequest("GET", "/?bucket=test-data&key=file.txt&"+parameter+"=", nil)
		err := objects.HandleInspectObjectDeletion(nil, httptest.NewRecorder(), r, nil)
		if err == nil || apperror.Public(err).Code != "InvalidDelete" {
			t.Fatalf("version-specific deletion accepted: %v", err)
		}
	}
	r := httptest.NewRequest("GET", "/?bucket=test-data&prefix=folder/&key=", nil)
	err := folders.HandleInspectFolderDeletion(nil, httptest.NewRecorder(), r, nil)
	if err == nil || apperror.Public(err).Code != "InvalidFolderDelete" {
		t.Fatalf("empty key treated as a folder listing: %v", err)
	}
}

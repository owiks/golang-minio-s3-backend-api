package storage

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"minio-web/api/src/services/apperror"
)

func TestConditionalCompletionContextIsScoped(t *testing.T) {
	condition := conditionalUpload{bucket: "bucket", key: "folder/a +%.txt", matchETag: `"checked"`}
	ctx := context.WithValue(context.Background(), conditionalUploadKey{}, condition)
	for _, tc := range []struct {
		name, method, path, query string
		marked, want              bool
	}{
		{"final", "POST", "/bucket/folder/a +%.txt", "uploadId=known", true, true},
		{"no context", "POST", "/bucket/folder/a +%.txt", "uploadId=known", false, false},
		{"other key", "POST", "/bucket/other", "uploadId=known", true, false},
		{"other bucket", "POST", "/other/folder/a +%.txt", "uploadId=known", true, false},
		{"initiate", "POST", "/bucket/folder/a +%.txt", "uploads=", true, false},
		{"part", "PUT", "/bucket/folder/a +%.txt", "uploadId=known&partNumber=1", true, false},
		{"abort", "DELETE", "/bucket/folder/a +%.txt", "uploadId=known", true, false},
		{"select", "POST", "/bucket/folder/a +%.txt", "select=&select-type=2", true, false},
		{"unknown query", "POST", "/bucket/folder/a +%.txt", "uploadId=known&future=true", true, false},
		{"duplicate ID", "POST", "/bucket/folder/a +%.txt", "uploadId=one&uploadId=two", true, false},
		{"empty ID", "POST", "/bucket/folder/a +%.txt", "uploadId=", true, false},
		{"malformed query", "POST", "/bucket/folder/a +%.txt", "uploadId=%zz", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			callCtx := context.Background()
			if tc.marked {
				callCtx = ctx
			}
			headers := make(http.Header)
			if err := restoreConditionalComplete(callCtx, tc.method, &url.URL{Path: tc.path, RawQuery: tc.query}, headers); err != nil {
				t.Fatal(err)
			}
			if (headers.Get("If-Match") == condition.matchETag) != tc.want || headers.Get("If-None-Match") != "" {
				t.Fatal("conditions leaked beyond the bound commit", headers)
			}
		})
	}
}

func TestConditionalCompletionTransportDoesNotMutateCallerRequest(t *testing.T) {
	ctx := context.WithValue(context.Background(), conditionalUploadKey{}, conditionalUpload{bucket: "bucket", key: "file", exceptETag: "*"})
	request, err := http.NewRequestWithContext(ctx, "POST", "http://fixture.invalid/bucket/file?uploadId=known", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	transport := s3ResponseTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r == request || r.Header.Get("If-None-Match") != "*" {
			t.Error("native condition missing or caller request reused")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if request.Header.Get("If-None-Match") != "" {
		t.Fatal("transport changed the SDK-owned request headers")
	}
}

func TestConditionalCompletionRejectsConflictingConditions(t *testing.T) {
	ctx := context.WithValue(context.Background(), conditionalUploadKey{}, conditionalUpload{bucket: "bucket", key: "file", exceptETag: "*"})
	for _, header := range []http.Header{{"If-Match": {`"other"`}}, {"If-None-Match": {`"other"`}}} {
		err := restoreConditionalComplete(ctx, "POST", &url.URL{Path: "/bucket/file", RawQuery: "uploadId=known"}, header)
		if err == nil || apperror.Public(err).Code != "ConditionalUploadConflict" {
			t.Fatal("conflicting native condition accepted", err)
		}
	}
}

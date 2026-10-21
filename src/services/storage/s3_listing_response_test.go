package storage

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/tests/testmessage"
)

func TestSDKListsRejectWrongRootAndMalformedDocuments(t *testing.T) {
	for _, operation := range []string{"buckets", "objects v1", "objects v2", "uploads", "parts"} {
		for _, payload := range []string{
			`<Error><Code>InternalError</Code><Message>PRIVATE-UPSTREAM</Message></Error>`,
			`<html>PRIVATE-PROXY</html>`, `<Wrong/>`, ``, `<ListBucketResult>`,
		} {
			t.Run(operation+"/"+payload, func(t *testing.T) {
				calls := 0
				body := &countedAdminBody{reader: strings.NewReader(payload)}
				client := s3Fixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: 200, Header: http.Header{}, ContentLength: -1, Body: body}, nil
				}))
				err := invokeSDKList(operation, client)
				if err == nil || apperror.Public(err).Code != "InvalidS3ListingResponse" || calls != 1 || body.closed != 1 {
					t.Fatalf("invalid listing accepted/retried: %v calls=%d closes=%d", err, calls, body.closed)
				}
				if strings.Contains(testmessage.Russian(apperror.Public(err).Message), "PRIVATE") {
					t.Fatal("upstream details exposed")
				}
			})
		}
	}
}
func invokeSDKList(operation string, client *minio.Client) error {
	ctx := context.Background()
	core := minio.Core{Client: client}
	switch operation {
	case "buckets":
		_, err := client.ListBuckets(ctx)
		return err
	case "objects v1":
		_, err := core.ListObjects("bucket", "", "", "/", 1000)
		return err
	case "objects v2":
		_, err := core.ListObjectsV2("bucket", "", "", "", "/", 1000)
		return err
	case "uploads":
		_, err := core.ListMultipartUploads(ctx, "bucket", "", "", "", "/", 1000)
		return err
	default:
		_, err := core.ListObjectParts(ctx, "bucket", "key", "upload", 0, 1000)
		return err
	}
}
func TestSDKListsAcceptNativeEmptyResults(t *testing.T) {
	for _, tc := range []struct{ operation, root string }{
		{"buckets", "ListAllMyBucketsResult"}, {"objects v1", "ListBucketResult"}, {"objects v2", "ListBucketResult"}, {"uploads", "ListMultipartUploadsResult"}, {"parts", "ListPartsResult"},
	} {
		for _, namespace := range []string{"", ` xmlns="http://s3.amazonaws.com/doc/2006-03-01/"`} {
			t.Run(tc.operation+namespace, func(t *testing.T) {
				body := `<?xml version="1.0"?><` + tc.root + namespace + `/>`
				client := s3Fixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Header: http.Header{}, ContentLength: -1, Body: io.NopCloser(strings.NewReader(body))}, nil
				}))
				if err := invokeSDKList(tc.operation, client); err != nil {
					t.Fatal("valid empty result rejected", err)
				}
			})
		}
	}
}
func TestListingRequiresExactlyOneCompleteDocument(t *testing.T) {
	for _, payload := range []string{`<ListBucketResult/><Error/>`, `<ListBucketResult/>garbage`, `garbage<ListBucketResult/>`, `<ListBucketResult><Contents></ListBucketResult>`} {
		if err := validateS3Listing([]byte(payload), "ListBucketResult"); err == nil {
			t.Fatal("malformed listing accepted", payload)
		}
	}
	if err := validateS3Listing([]byte(" <!-- native --> <ListBucketResult/> \n"), "ListBucketResult"); err != nil {
		t.Fatal(err)
	}
}

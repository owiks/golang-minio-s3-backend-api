package storage

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestVersionResponseRequiresOneCompleteDocument(t *testing.T) {
	valid := `<ListVersionsResult><Version><Key>file</Key><VersionId>v1</VersionId></Version></ListVersionsResult>`
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"native", valid, true},
		{"native with UTF-8 BOM", "\ufeff" + valid, true},
		{"comments and whitespace", "<?xml version=\"1.0\"?>\n<!-- before -->" + valid + "\n<!-- after --> ", true},
		{"empty", "", false},
		{"second error document", valid + `<Error><Code>AccessDenied</Code></Error>`, false},
		{"second listing", valid + `<ListVersionsResult/>`, false},
		{"trailing text", valid + "PRIVATE-UPSTREAM", false},
		{"trailing BOM", valid + "\ufeff", false},
		{"leading text", "PRIVATE-UPSTREAM" + valid, false},
		{"trailing broken XML", valid + "<", false},
		{"trailing declaration", valid + `<?xml version="1.0"?>`, false},
		{"trailing directive", valid + `<!DOCTYPE other>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedBody{Reader: strings.NewReader(tc.body)}
			calls := 0
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Body: body}, nil
			})
			page, err := ReadVersionPage(context.Background(), s3Fixture(t, transport), transport, "test-bucket", "file", "", "", 50)
			if !body.closed || calls != 1 {
				t.Fatal("response not closed or retried", calls, body.closed)
			}
			if tc.valid {
				if err != nil || len(page.Rows) != 1 || page.Rows[0].ID != "v1" {
					t.Fatal("valid native response changed", page, err)
				}
			} else if !errors.Is(err, ErrInvalidVersionResponse) || len(page.Rows) != 0 || page.Truncated || page.KeyMarker != "" || page.VersionMarker != "" {
				t.Fatal("invalid response became a successful/partial page", page, err)
			}
		})
	}
}

func TestVersionResponseDecoderPreservesEmptyNativeDocuments(t *testing.T) {
	for _, body := range []string{`<ListVersionsResult/>`, `<?xml version="1.0"?><s3:ListVersionsResult xmlns:s3="http://s3.amazonaws.com/doc/2006-03-01/"><FutureField><Child/></FutureField></s3:ListVersionsResult>`} {
		page, err := decodeVersionPage(strings.NewReader(body))
		if err != nil || !reflect.DeepEqual(page, VersionPage{}) {
			t.Fatal("empty/native namespace or unknown field rejected", page, err)
		}
	}
	if _, err := decodeVersionPage(io.MultiReader(strings.NewReader(`<ListVersionsResult/>`), failedVersionReader{})); !errors.Is(err, ErrInvalidVersionResponse) {
		t.Fatal("unread failing suffix accepted", err)
	}
}

type failedVersionReader struct{}

func (failedVersionReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestVersionPageRejectsUnexpectedRoot(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"MinIO error", `<Error><Code>AccessDenied</Code><Message>PRIVATE-UPSTREAM</Message></Error>`},
		{"namespaced error", `<Error xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Code>InternalError</Code></Error>`},
		{"prefixed error", `<s3:Error xmlns:s3="http://s3.amazonaws.com/doc/2006-03-01/"><Code>SlowDown</Code></s3:Error>`},
		{"HTML", `<html><body>PRIVATE-UPSTREAM</body></html>`},
		{"object listing", `<ListBucketResult/>`},
		{"wrong case", `<listversionsresult/>`},
		{"wrapped listing", `<Envelope><ListVersionsResult/></Envelope>`},
		{"rows under wrong root", `<Error><Version><Key>file</Key><VersionId>v1</VersionId></Version><IsTruncated>true</IsTruncated><NextKeyMarker>file</NextKeyMarker><NextVersionIdMarker>v1</NextVersionIdMarker></Error>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var page VersionPage
			err := xml.Unmarshal([]byte(tc.body), &page)
			if !errors.Is(err, ErrInvalidVersionResponse) || len(page.Rows) != 0 || page.Truncated || page.KeyMarker != "" || page.VersionMarker != "" {
				t.Fatal("unexpected XML accepted or populated a version page", page, err)
			}
		})
	}
}

func TestVersionPageAcceptsNativeRootAndPreservesRowsAndMarkers(t *testing.T) {
	for _, tc := range []struct{ name, root, prefix string }{
		{"unqualified", "ListVersionsResult", ""},
		{"default namespace", `ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"`, ""},
		{"namespace prefix", `s3:ListVersionsResult xmlns:s3="http://s3.amazonaws.com/doc/2006-03-01/"`, "s3:"},
	} {
		for _, empty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/empty=%t", tc.name, empty), func(t *testing.T) {
				end := tc.prefix + "ListVersionsResult"
				body := "<?xml version=\"1.0\"?>\n<!-- native S3 listing -->\n<" + tc.root + ">"
				want := VersionPage{}
				if !empty {
					// Unknown native fields must remain forward compatible. The
					// mixed version/delete-marker ordering and opaque cursors matter.
					for _, part := range []string{
						`<Name>bucket</Name><EncodingType>url</EncodingType><FutureField><Child/></FutureField>`,
						`<Version><Key>a%2B</Key><VersionId>v+/=</VersionId><IsLatest>true</IsLatest><Size>12</Size><Owner><ID>owner</ID></Owner></Version>`,
						`<DeleteMarker><Key>a%2B</Key><VersionId>deleted</VersionId></DeleteMarker>`,
						`<Version><Key>a%2B</Key><VersionId>older</VersionId><Size>4</Size></Version>`,
						`<IsTruncated>true</IsTruncated><NextKeyMarker>a%2B</NextKeyMarker><NextVersionIdMarker>v+/=</NextVersionIdMarker>`,
					} {
						body += part
					}
					want = VersionPage{Rows: []VersionRow{{Key: "a%2B", ID: "v+/=", Latest: true, Size: 12}, {Key: "a%2B", ID: "deleted", Deleted: true}, {Key: "a%2B", ID: "older", Size: 4}}, Truncated: true, KeyMarker: "a%2B", VersionMarker: "v+/="}
				}
				body += "</" + end + ">"
				var page VersionPage
				if err := xml.Unmarshal([]byte(body), &page); err != nil || !reflect.DeepEqual(page, want) {
					t.Fatal("native listing changed", page, want, err)
				}
			})
		}
	}
}

func TestVersionPageRejectsMalformedNativeListing(t *testing.T) {
	for _, body := range []string{
		`<ListVersionsResult>`,
		`<ListVersionsResult><Version><Key>file</Key></Version>`,
		`<ListVersionsResult><Version><Size>not-a-number</Size></Version></ListVersionsResult>`,
		`<ListVersionsResult><IsTruncated>not-a-bool</IsTruncated></ListVersionsResult>`,
		`<ListVersionsResult><FutureField></ListVersionsResult>`,
	} {
		var page VersionPage
		if err := xml.Unmarshal([]byte(body), &page); err == nil {
			t.Fatal("invalid native listing accepted", body)
		}
	}
}

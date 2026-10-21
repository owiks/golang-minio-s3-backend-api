package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"minio-web/api/src/services/apperror"
)

const attributesModified = "Tue, 20 Oct 2026 10:00:00 GMT"

func objectAttributesResponse(status int, body io.ReadCloser, version string) *http.Response {
	header := http.Header{"Last-Modified": {attributesModified}, "Content-Type": {"application/xml"}}
	if version != "" {
		header.Set("X-Amz-Version-Id", version)
	}
	return &http.Response{StatusCode: status, Header: header, ContentLength: -1, Body: body}
}

func readObjectAttributesFixture(t *testing.T, payload string, opts ObjectAttributesOptions) (ObjectAttributesPage, error, int, bool) {
	t.Helper()
	calls := 0
	body := &trackedBody{Reader: strings.NewReader(payload)}
	tr := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return objectAttributesResponse(200, body, opts.VersionID), nil
	})
	page, err := NewS3Client(s3Fixture(t, tr)).ReadObjectAttributes(context.Background(), "test-bucket", "folder/file", opts)
	return page, err, calls, body.closed
}

func TestObjectAttributesUsesSDKRequestAndPreservesVersion(t *testing.T) {
	const version = "old+/=version"
	const key = "folder/a +%?file"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/test-bucket/"+key || !r.URL.Query().Has("attributes") || r.URL.Query().Get("versionId") != version {
			t.Error("SDK changed the requested object/version", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") == "" || r.Header.Get("X-Amz-Max-Parts") != "2" || !strings.Contains(r.Header.Get("X-Amz-Object-Attributes"), "ObjectParts") {
			t.Error("SDK signature or attributes pagination headers missing", r.Header)
		}
		w.Header().Set("Last-Modified", attributesModified)
		w.Header().Set("X-Amz-Version-Id", version)
		_, _ = io.WriteString(w, `<GetObjectAttributesOutput xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ETag>"opaque-etag-2"</ETag><ObjectSize>8388612</ObjectSize><StorageClass>STANDARD</StorageClass><Checksum><ChecksumCRC32C>AAAAAA==</ChecksumCRC32C><ChecksumType>FULL_OBJECT</ChecksumType></Checksum><ObjectParts><PartsCount>2</PartsCount><PartNumberMarker>0</PartNumberMarker><NextPartNumberMarker>0</NextPartNumberMarker><MaxParts>2</MaxParts><IsTruncated>false</IsTruncated><Part><PartNumber>1</PartNumber><Size>8388608</Size><ChecksumCRC32C>AAAAAQ==</ChecksumCRC32C></Part><Part><PartNumber>2</PartNumber><Size>4</Size></Part></ObjectParts></GetObjectAttributesOutput>`)
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	client, err := NewS3(Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4("access", "secret", ""), Region: "us-east-1"}, server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	page, err := NewS3Client(client).ReadObjectAttributes(context.Background(), "test-bucket", key, ObjectAttributesOptions{VersionID: version, MaxParts: 2})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || page.VersionID != version || page.ETag != "opaque-etag-2" || page.Size == nil || *page.Size != 8388612 || page.StorageClass != "STANDARD" || page.PartsCount == nil || *page.PartsCount != 2 {
		t.Fatal("native attributes lost, extra requests or fallback download", calls.Load(), page)
	}
	if page.ChecksumType != "FULL_OBJECT" || page.Checksums["x-amz-checksum-crc32c"] != "AAAAAA==" || len(page.Parts) != 2 || page.Parts[0].Number != 1 || page.Parts[0].Size != 8388608 || page.Parts[0].Checksums["x-amz-checksum-crc32c"] != "AAAAAQ==" || page.Truncated == nil || *page.Truncated || page.NextPartNumberMarker != nil {
		t.Fatal("checksum/parts contract changed", page)
	}
	if page.LastModified.IsZero() || page.LastModified.Format(http.TimeFormat) != attributesModified {
		t.Fatal("last modified lost", page.LastModified)
	}
}

func TestObjectAttributesDistinguishesMissingFromExplicitZero(t *testing.T) {
	for _, tc := range []struct {
		name, xml string
		present   bool
	}{
		{"omitted", `<GetObjectAttributesOutput/>`, false},
		{"explicit zero", `<GetObjectAttributesOutput><ObjectSize>0</ObjectSize><ObjectParts><PartsCount>0</PartsCount><IsTruncated>false</IsTruncated></ObjectParts></GetObjectAttributesOutput>`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err, calls, closed := readObjectAttributesFixture(t, tc.xml, ObjectAttributesOptions{MaxParts: 100})
			if err != nil || calls != 1 || !closed || (page.Size != nil) != tc.present || (page.PartsCount != nil) != tc.present {
				t.Fatal("absent values became confirmed zero or response cleanup lost", page, err, calls, closed)
			}
			if page.Size != nil && *page.Size != 0 || page.PartsCount != nil && *page.PartsCount != 0 || len(page.Parts) != 0 || len(page.Checksums) != 0 || page.ChecksumType != "" || (page.Parts != nil) != tc.present || (page.Truncated != nil) != tc.present || page.Truncated != nil && *page.Truncated {
				t.Fatal("empty attributes were invented", page)
			}
			encoded, err := json.Marshal(page)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			_, sizePresent := fields["size"]
			_, countPresent := fields["partsCount"]
			wantParts := "null"
			if tc.present {
				wantParts = "[]"
			}
			if sizePresent != tc.present || countPresent != tc.present || string(fields["parts"]) != wantParts {
				t.Fatal("JSON advertised missing metadata as a confirmed value", string(encoded))
			}
		})
	}
}

func TestObjectAttributesDoesNotInferChecksumTypeFromETag(t *testing.T) {
	for _, etag := range []string{"single-part-etag", "multipart-etag-2"} {
		body := `<GetObjectAttributesOutput><ETag>` + etag + `</ETag><Checksum><ChecksumSHA256>AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=</ChecksumSHA256></Checksum></GetObjectAttributesOutput>`
		page, err, _, _ := readObjectAttributesFixture(t, body, ObjectAttributesOptions{MaxParts: 10})
		if err != nil || page.ChecksumType != "" || len(page.Checksums) != 1 {
			t.Fatal("checksum mode was inferred from ETag", page, err)
		}
	}
}

func TestObjectAttributesAcceptsNativeXMLVariants(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"default namespace", `<GetObjectAttributesOutput xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ObjectSize>0</ObjectSize></GetObjectAttributesOutput>`},
		{"namespace prefix", `<s3:GetObjectAttributesOutput xmlns:s3="http://s3.amazonaws.com/doc/2006-03-01/"><s3:ObjectSize>0</s3:ObjectSize></s3:GetObjectAttributesOutput>`},
		{"native MinIO root", `<getObjectAttributesResponse><ObjectSize>0</ObjectSize></getObjectAttributesResponse>`},
		{"native MinIO namespace", `<getObjectAttributesResponse xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ObjectSize>0</ObjectSize></getObjectAttributesResponse>`},
		{"comments", `<?xml version="1.0"?><!-- before --><GetObjectAttributesOutput><ObjectSize>0</ObjectSize></GetObjectAttributesOutput><!-- after -->`},
		{"UTF-8 BOM", "\ufeff" + `<GetObjectAttributesOutput><ObjectSize>0</ObjectSize></GetObjectAttributesOutput>`},
		{"future scalar and nested fields", `<GetObjectAttributesOutput><ObjectSize>0</ObjectSize><FutureField><FutureNested>value</FutureNested></FutureField></GetObjectAttributesOutput>`},
		{"future checksum mode", `<GetObjectAttributesOutput><ObjectSize>0</ObjectSize><Checksum><ChecksumType>FUTURE_MODE</ChecksumType></Checksum></GetObjectAttributesOutput>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err, calls, closed := readObjectAttributesFixture(t, tc.body, ObjectAttributesOptions{MaxParts: 10})
			if err != nil || page.Size == nil || *page.Size != 0 || calls != 1 || !closed || page.ChecksumType != "" {
				t.Fatal("valid native XML changed or unknown mode advertised", page, err, calls, closed)
			}
		})
	}
}

func TestObjectAttributesAcceptsNativeTerminalPartMarker(t *testing.T) {
	for _, root := range []string{"GetObjectAttributesOutput", "getObjectAttributesResponse"} {
		for _, marker := range []int{0, 1} {
			t.Run(fmt.Sprintf("%s/from-%d", root, marker), func(t *testing.T) {
				parts := `<Part><PartNumber>2</PartNumber><Size>5</Size></Part>`
				if marker == 0 {
					parts = `<Part><PartNumber>1</PartNumber><Size>3</Size></Part>` + parts
				}
				// MinIO may repeat the last returned part as its next marker even
				// when IsTruncated=false. That is a terminal page, not continuation.
				body := fmt.Sprintf(`<%s><ObjectSize>8</ObjectSize><ObjectParts><PartsCount>2</PartsCount><PartNumberMarker>%d</PartNumberMarker><NextPartNumberMarker>2</NextPartNumberMarker><MaxParts>2</MaxParts><IsTruncated>false</IsTruncated>%s</ObjectParts></%s>`, root, marker, parts, root)
				page, err, calls, closed := readObjectAttributesFixture(t, body, ObjectAttributesOptions{MaxParts: 2, PartNumberMarker: marker})
				if err != nil || calls != 1 || !closed || page.Truncated == nil || *page.Truncated || page.NextPartNumberMarker != nil || page.PartsCount == nil || *page.PartsCount != 2 || len(page.Parts) != 2-marker || page.Parts[len(page.Parts)-1].Number != 2 {
					t.Fatal("native terminal marker rejected or exposed as continuation", page, err, calls, closed)
				}
			})
		}
	}
}

func TestObjectAttributesReadsOnePageAndPreservesNativeMarker(t *testing.T) {
	const version = "old-version"
	calls := 0
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("versionId") != version || r.Header.Get("X-Amz-Max-Parts") != "1" {
			t.Fatal("version or page size changed", r.URL, r.Header)
		}
		marker := r.Header.Get("X-Amz-Part-Number-Marker")
		body := `<GetObjectAttributesOutput><ObjectParts><PartsCount>2</PartsCount><PartNumberMarker>0</PartNumberMarker><NextPartNumberMarker>1</NextPartNumberMarker><MaxParts>1</MaxParts><IsTruncated>true</IsTruncated><Part><PartNumber>1</PartNumber><Size>5242880</Size></Part></ObjectParts></GetObjectAttributesOutput>`
		if calls == 1 && marker != "" || calls == 2 && marker != "1" {
			t.Fatal("marker was rewritten", calls, marker)
		}
		if calls == 2 {
			body = `<GetObjectAttributesOutput><ObjectParts><PartsCount>2</PartsCount><PartNumberMarker>1</PartNumberMarker><MaxParts>1</MaxParts><IsTruncated>false</IsTruncated><Part><PartNumber>2</PartNumber><Size>3</Size></Part></ObjectParts></GetObjectAttributesOutput>`
		}
		return objectAttributesResponse(200, io.NopCloser(strings.NewReader(body)), version), nil
	})
	client := NewS3Client(s3Fixture(t, tr))
	first, err := client.ReadObjectAttributes(context.Background(), "test-bucket", "file", ObjectAttributesOptions{VersionID: version, MaxParts: 1})
	if err != nil || calls != 1 || first.Truncated == nil || !*first.Truncated || first.NextPartNumberMarker == nil || *first.NextPartNumberMarker != 1 || len(first.Parts) != 1 {
		t.Fatal("page was auto-drained or continuation lost", first, err, calls)
	}
	last, err := client.ReadObjectAttributes(context.Background(), "test-bucket", "file", ObjectAttributesOptions{VersionID: version, MaxParts: 1, PartNumberMarker: *first.NextPartNumberMarker})
	if err != nil || calls != 2 || last.Truncated == nil || *last.Truncated || last.NextPartNumberMarker != nil || len(last.Parts) != 1 || last.Parts[0].Number != 2 || last.VersionID != first.VersionID {
		t.Fatal("last page or immutable version changed", last, err, calls)
	}
}

func TestObjectAttributesRejectsMalformedDocumentsAndParts(t *testing.T) {
	const valid = `<GetObjectAttributesOutput><ObjectSize>5</ObjectSize></GetObjectAttributesOutput>`
	for _, tc := range []struct{ name, body string }{
		{"empty", ""},
		{"HTML", `<html><body>PRIVATE-UPSTREAM</body></html>`},
		{"success error", `<Error><Code>AccessDenied</Code><Message>PRIVATE-UPSTREAM</Message></Error>`},
		{"wrapped root", `<Envelope>` + valid + `</Envelope>`},
		{"wrong root", `<ObjectAttributesOutput/>`},
		{"unsupported root spelling", `<GetObjectAttributesResponse/>`},
		{"wrong case MinIO root", `<getobjectattributesresponse/>`},
		{"truncated root", `<GetObjectAttributesOutput>`},
		{"second document", valid + `<Error><Code>AccessDenied</Code></Error>`},
		{"trailing text", valid + `PRIVATE-UPSTREAM`},
		{"trailing broken XML", valid + `<`},
		{"trailing declaration", valid + `<?xml version="1.0"?>`},
		{"trailing BOM", valid + "\ufeff"},
		{"directive", `<!DOCTYPE GetObjectAttributesOutput>` + valid},
		{"duplicate size", `<GetObjectAttributesOutput><ObjectSize>5</ObjectSize><ObjectSize>6</ObjectSize></GetObjectAttributesOutput>`},
		{"duplicate parts block", `<GetObjectAttributesOutput><ObjectParts/><ObjectParts/></GetObjectAttributesOutput>`},
		{"duplicate checksum", `<GetObjectAttributesOutput><Checksum><ChecksumCRC32C>AAAAAA==</ChecksumCRC32C><ChecksumCRC32C>AAAAAQ==</ChecksumCRC32C></Checksum></GetObjectAttributesOutput>`},
		{"negative size", `<GetObjectAttributesOutput><ObjectSize>-1</ObjectSize></GetObjectAttributesOutput>`},
		{"overflow size", `<GetObjectAttributesOutput><ObjectSize>9223372036854775808</ObjectSize></GetObjectAttributesOutput>`},
		{"nil-equivalent part", `<GetObjectAttributesOutput><ObjectParts><Part/></ObjectParts></GetObjectAttributesOutput>`},
		{"explicit nil part", `<GetObjectAttributesOutput xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><ObjectParts><Part xsi:nil="true"/></ObjectParts></GetObjectAttributesOutput>`},
		{"part missing size", `<GetObjectAttributesOutput><ObjectParts><Part><PartNumber>1</PartNumber></Part></ObjectParts></GetObjectAttributesOutput>`},
		{"part missing number", `<GetObjectAttributesOutput><ObjectParts><Part><Size>3</Size></Part></ObjectParts></GetObjectAttributesOutput>`},
		{"negative part size", `<GetObjectAttributesOutput><ObjectParts><Part><PartNumber>1</PartNumber><Size>-1</Size></Part></ObjectParts></GetObjectAttributesOutput>`},
		{"zero number", `<GetObjectAttributesOutput><ObjectParts><Part><PartNumber>0</PartNumber><Size>3</Size></Part></ObjectParts></GetObjectAttributesOutput>`},
		{"number exceeds S3 limit", `<GetObjectAttributesOutput><ObjectParts><Part><PartNumber>10001</PartNumber><Size>3</Size></Part></ObjectParts></GetObjectAttributesOutput>`},
		{"duplicate part", `<GetObjectAttributesOutput><ObjectParts><Part><PartNumber>1</PartNumber><Size>3</Size></Part><Part><PartNumber>1</PartNumber><Size>3</Size></Part></ObjectParts></GetObjectAttributesOutput>`},
		{"out of order part", `<GetObjectAttributesOutput><ObjectParts><Part><PartNumber>2</PartNumber><Size>3</Size></Part><Part><PartNumber>1</PartNumber><Size>3</Size></Part></ObjectParts></GetObjectAttributesOutput>`},
		{"negative total", `<GetObjectAttributesOutput><ObjectParts><PartsCount>-1</PartsCount></ObjectParts></GetObjectAttributesOutput>`},
		{"total smaller than page", `<GetObjectAttributesOutput><ObjectParts><PartsCount>0</PartsCount><Part><PartNumber>1</PartNumber><Size>3</Size></Part></ObjectParts></GetObjectAttributesOutput>`},
		{"incomplete first page marked complete", `<GetObjectAttributesOutput><ObjectParts><PartsCount>2</PartsCount><IsTruncated>false</IsTruncated><Part><PartNumber>1</PartNumber><Size>3</Size></Part></ObjectParts></GetObjectAttributesOutput>`},
		{"missing first page marked complete", `<GetObjectAttributesOutput><ObjectParts><PartsCount>1</PartsCount><IsTruncated>false</IsTruncated></ObjectParts></GetObjectAttributesOutput>`},
		{"truncation without marker", `<GetObjectAttributesOutput><ObjectParts><IsTruncated>true</IsTruncated><Part><PartNumber>1</PartNumber><Size>3</Size></Part></ObjectParts></GetObjectAttributesOutput>`},
		{"truncation without parts", `<GetObjectAttributesOutput><ObjectParts><IsTruncated>true</IsTruncated><NextPartNumberMarker>1</NextPartNumberMarker></ObjectParts></GetObjectAttributesOutput>`},
		{"marker exceeds parts", `<GetObjectAttributesOutput><ObjectParts><IsTruncated>true</IsTruncated><NextPartNumberMarker>4</NextPartNumberMarker><Part><PartNumber>1</PartNumber><Size>3</Size></Part></ObjectParts></GetObjectAttributesOutput>`},
		{"terminal marker skips past final part", `<GetObjectAttributesOutput><ObjectParts><IsTruncated>false</IsTruncated><NextPartNumberMarker>2</NextPartNumberMarker><Part><PartNumber>1</PartNumber><Size>3</Size></Part></ObjectParts></GetObjectAttributesOutput>`},
		{"terminal marker without parts", `<GetObjectAttributesOutput><ObjectParts><IsTruncated>false</IsTruncated><NextPartNumberMarker>1</NextPartNumberMarker></ObjectParts></GetObjectAttributesOutput>`},
		{"invalid truncated boolean", `<GetObjectAttributesOutput><ObjectParts><IsTruncated>garbage</IsTruncated></ObjectParts></GetObjectAttributesOutput>`},
		{"checksum container text", `<GetObjectAttributesOutput><Checksum>PRIVATE-UPSTREAM<ChecksumType>FULL_OBJECT</ChecksumType></Checksum></GetObjectAttributesOutput>`},
		{"unknown parts tail", `<GetObjectAttributesOutput><ObjectParts><PartsCount>3</PartsCount><IsTruncated>false</IsTruncated><Part><PartNumber>1</PartNumber><Size>3</Size></Part><Part><PartNumber>2</PartNumber><Size>3</Size></Part><Part><PartNumber>3</PartNumber><Size>3</Size></Part></ObjectParts></GetObjectAttributesOutput>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err, calls, closed := readObjectAttributesFixture(t, tc.body, ObjectAttributesOptions{MaxParts: 2})
			if !IsInvalidObjectAttributes(err) || calls != 1 || !closed || !reflect.DeepEqual(page, ObjectAttributesPage{}) {
				t.Fatal("invalid metadata accepted, retried or partially returned", page, err, calls, closed)
			}
			if strings.Contains(apperror.Public(err).Message, "PRIVATE-UPSTREAM") {
				t.Fatal("raw upstream content leaked")
			}
		})
	}
}

func TestObjectAttributesRejectsNonAdvancingOrMismatchedMarker(t *testing.T) {
	for _, part := range []string{
		`<PartNumberMarker>1</PartNumberMarker><IsTruncated>true</IsTruncated><NextPartNumberMarker>1</NextPartNumberMarker><Part><PartNumber>2</PartNumber><Size>3</Size></Part>`,
		`<PartNumberMarker>0</PartNumberMarker><IsTruncated>false</IsTruncated><Part><PartNumber>2</PartNumber><Size>3</Size></Part>`,
		`<PartNumberMarker>1</PartNumberMarker><IsTruncated>false</IsTruncated><Part><PartNumber>1</PartNumber><Size>3</Size></Part>`,
		`<PartNumberMarker>1</PartNumberMarker><IsTruncated>false</IsTruncated><NextPartNumberMarker>1</NextPartNumberMarker><Part><PartNumber>2</PartNumber><Size>3</Size></Part>`,
		`<PartNumberMarker>1</PartNumberMarker><IsTruncated>false</IsTruncated><NextPartNumberMarker>3</NextPartNumberMarker><Part><PartNumber>2</PartNumber><Size>3</Size></Part>`,
	} {
		_, err, calls, closed := readObjectAttributesFixture(t, `<GetObjectAttributesOutput><ObjectParts>`+part+`</ObjectParts></GetObjectAttributesOutput>`, ObjectAttributesOptions{MaxParts: 1, PartNumberMarker: 1})
		if !IsInvalidObjectAttributes(err) || calls != 1 || !closed {
			t.Fatal("non-advancing response accepted", err, calls, closed)
		}
	}
}

func TestObjectAttributesRejectsVersionSubstitution(t *testing.T) {
	for _, actualVersion := range []string{"", "new-current", "null"} {
		tr := roundTripFunc(func(*http.Request) (*http.Response, error) {
			return objectAttributesResponse(200, io.NopCloser(strings.NewReader(`<GetObjectAttributesOutput><ObjectSize>3</ObjectSize></GetObjectAttributesOutput>`)), actualVersion), nil
		})
		_, err := NewS3Client(s3Fixture(t, tr)).ReadObjectAttributes(context.Background(), "test-bucket", "file", ObjectAttributesOptions{VersionID: "old-version", MaxParts: 10})
		if !IsInvalidObjectAttributes(err) {
			t.Fatal("requested historical version silently replaced", actualVersion, err)
		}
	}
}

func TestObjectAttributesNativeErrorsRemainDistinctWithoutFallback(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{
		{403, "AccessDenied"},
		{404, "NoSuchKey"},
		{404, "NoSuchVersion"},
		{501, "NotImplemented"},
		{503, "SlowDown"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			calls := 0
			body := &trackedBody{Reader: strings.NewReader(`<Error><Code>` + tc.code + `</Code><Message>PRIVATE-UPSTREAM</Message></Error>`)}
			tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if !r.URL.Query().Has("attributes") {
					t.Fatal("unexpected download fallback", r.URL)
				}
				return objectAttributesResponse(tc.status, body, ""), nil
			})
			page, err := NewS3Client(s3Fixture(t, tr)).ReadObjectAttributes(context.Background(), "test-bucket", "file", ObjectAttributesOptions{MaxParts: 10})
			code := minio.ToErrorResponse(err).Code
			if tc.code == "NoSuchVersion" {
				code = apperror.Public(err).Code
			}
			if err == nil || code != tc.code || calls != 1 || !body.closed || !reflect.DeepEqual(page, ObjectAttributesPage{}) {
				t.Fatal("native errors collapsed, retried or partial response exposed", page, err, code, calls, body.closed)
			}
		})
	}
}

func TestObjectAttributesLimitsMetadataBeforeSDKDecoding(t *testing.T) {
	for _, declared := range []int64{-1, maxS3MetadataResponse + 1} {
		t.Run(strconv.FormatInt(declared, 10), func(t *testing.T) {
			body := &countedAdminBody{reader: io.LimitReader(zeroReader{}, maxS3MetadataResponse+4096)}
			calls := 0
			tr := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				res := objectAttributesResponse(200, body, "")
				res.ContentLength = declared
				return res, nil
			})
			_, err := NewS3Client(s3Fixture(t, tr)).ReadObjectAttributes(context.Background(), "test-bucket", "file", ObjectAttributesOptions{MaxParts: 1})
			if err == nil || apperror.Public(err).Code != "S3ResponseTooLarge" || calls != 1 || body.closed != 1 || body.read > maxS3MetadataResponse+1 || declared > 0 && body.read != 0 {
				t.Fatal("unbounded read or size error lost", err, calls, body.closed, body.read)
			}
		})
	}
}

func TestObjectAttributesPreservesReadFailures(t *testing.T) {
	for _, failure := range []error{io.ErrUnexpectedEOF, context.Canceled, context.DeadlineExceeded} {
		t.Run(failure.Error(), func(t *testing.T) {
			body := &countedAdminBody{reader: io.MultiReader(strings.NewReader(`<GetObjectAttributesOutput/>`), failedS3Read{err: failure})}
			calls := 0
			tr := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return objectAttributesResponse(200, body, ""), nil
			})
			_, err := NewS3Client(s3Fixture(t, tr)).ReadObjectAttributes(context.Background(), "test-bucket", "file", ObjectAttributesOptions{MaxParts: 1})
			if !errors.Is(err, failure) || calls != 1 || body.closed != 1 {
				t.Fatal("body failure accepted or cleanup/retry invariant lost", err, calls, body.closed)
			}
		})
	}
}

func TestObjectAttributesCancellationReachesUpstream(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Last-Modified", attributesModified)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	client, err := NewS3(Config{Endpoint: endpoint, Credentials: credentials.NewStaticV4("access", "secret", ""), Region: "us-east-1"}, server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := NewS3Client(client).ReadObjectAttributes(ctx, "test-bucket", "file", ObjectAttributesOptions{MaxParts: 1})
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("native request did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation lost", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled attributes request did not finish")
	}
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not reach upstream")
	}
	if calls.Load() != 1 {
		t.Fatal("canceled request retried", calls.Load())
	}
}

func TestObjectAttributesRejectsInvalidPaginationBeforeUpstream(t *testing.T) {
	for _, opts := range []ObjectAttributesOptions{{MaxParts: 0}, {MaxParts: -1}, {MaxParts: 1001}, {MaxParts: 1, PartNumberMarker: -1}, {MaxParts: 1, PartNumberMarker: 10001}} {
		calls := 0
		client := NewS3Client(s3Fixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("must not execute")
		})))
		_, err := client.ReadObjectAttributes(context.Background(), "test-bucket", "file", opts)
		if err == nil || apperror.Public(err).Status != 400 || calls != 0 {
			t.Fatal("invalid pagination reached storage", opts, err, calls)
		}
	}
}

func TestObjectAttributesConcurrentReadsKeepPresenceIsolated(t *testing.T) {
	client := NewS3Client(s3Fixture(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		n, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/test-bucket/file-"))
		if err != nil {
			return nil, err
		}
		body := `<GetObjectAttributesOutput/>`
		if n%2 == 0 {
			body = fmt.Sprintf(`<GetObjectAttributesOutput><ObjectSize>%d</ObjectSize></GetObjectAttributesOutput>`, n)
		}
		return objectAttributesResponse(200, io.NopCloser(strings.NewReader(body)), ""), nil
	})))
	var workers sync.WaitGroup
	errors := make(chan error, 32)
	for i := range 32 {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			page, err := client.ReadObjectAttributes(context.Background(), "test-bucket", fmt.Sprintf("file-%d", i), ObjectAttributesOptions{MaxParts: 1})
			if err != nil {
				errors <- err
				return
			}
			if i%2 == 0 && (page.Size == nil || *page.Size != int64(i)) || i%2 != 0 && page.Size != nil {
				errors <- fmt.Errorf("response capture crossed concurrent reads: file %d: %+v", i, page)
			}
		}(i)
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestObjectAttributesAllowsServerLowerPageCap(t *testing.T) {
	body := `<GetObjectAttributesOutput><ObjectParts><PartsCount>2</PartsCount><MaxParts>1</MaxParts><IsTruncated>true</IsTruncated><NextPartNumberMarker>1</NextPartNumberMarker><Part><PartNumber>1</PartNumber><Size>3</Size></Part></ObjectParts></GetObjectAttributesOutput>`
	page, err, calls, closed := readObjectAttributesFixture(t, body, ObjectAttributesOptions{MaxParts: 2})
	if err != nil || len(page.Parts) != 1 || calls != 1 || !closed {
		t.Fatal(page, err, calls, closed)
	}
}

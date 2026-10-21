package storage

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

const (
	maxS3MetadataResponse = 4 << 20
	maxS3ErrorResponse    = 64 << 10
)

// NewS3 uses path-style addressing. Only a successful object GET with known
// download selectors is a file stream. Bucket operations, object subresources
// and new/unknown API queries remain finite, regardless of their Content-Type.
func s3ResponseLimit(method string, u *url.URL, status int) int64 {
	if method == http.MethodHead {
		return 0 // Content-Length describes the object, not the HEAD body.
	}
	if status < 200 || status >= 300 {
		return maxS3ErrorResponse
	}
	if method != http.MethodGet || status != http.StatusOK && status != http.StatusPartialContent {
		return maxS3MetadataResponse
	}
	_, key, object := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	if !object || key == "" {
		return maxS3MetadataResponse
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return maxS3MetadataResponse
	}
	for name := range q {
		switch name {
		case "versionId", "partNumber",
			"response-cache-control", "response-content-disposition", "response-content-encoding",
			"response-content-language", "response-content-type", "response-expires",
			"X-Amz-Algorithm", "X-Amz-Credential", "X-Amz-Date", "X-Amz-Expires",
			"X-Amz-SignedHeaders", "X-Amz-Signature", "X-Amz-Security-Token",
			"AWSAccessKeyId", "Signature", "Expires", "SecurityToken":
		default:
			return maxS3MetadataResponse
		}
	}
	return 0
}

// Consume finite responses before the SDK can decode a valid prefix or ignore
// a command's body. Never drain a rejected body during SDK cleanup. In contrast
// to madmin, this S3 client has MaxRetries=1, so a transport error cannot replay
// a command and also cannot be hidden by SDK methods that ignore success bodies.
type s3ResponseTransport struct{ base http.RoundTripper }

// The SDK rewrites transport errors containing "EOF", losing their cause.
// Keep read failures inspectable without exposing the raw upstream error text.
type s3MetadataReadError struct{ cause error }

func (e s3MetadataReadError) Error() string { return "S3 metadata response read failed" }
func (e s3MetadataReadError) Unwrap() error { return e.cause }

func (t s3ResponseTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if _, ok := r.Context().Value(conditionalUploadKey{}).(conditionalUpload); ok {
		// A RoundTripper must not mutate the caller's request headers.
		r = r.Clone(r.Context())
	}
	if err := restoreConditionalComplete(r.Context(), r.Method, r.URL, r.Header); err != nil {
		return nil, err
	}
	response, err := t.base.RoundTrip(r)
	if err != nil || response == nil || response.Body == nil {
		return response, err
	}
	if wrapped, matched, selectErr := selectResponse(r.Context(), r.Method, r.URL, response); matched {
		return wrapped, selectErr
	}
	limit := s3ResponseLimit(r.Method, r.URL, response.StatusCode)
	if limit == 0 {
		return response, nil
	}
	body := response.Body
	defer body.Close()
	tooLarge := func() error {
		return apperror.New(502, "S3ResponseTooLarge", messages.StorageS3ResponseTooLarge)
	}
	if response.ContentLength > limit {
		return nil, tooLarge()
	}
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if int64(len(data)) > limit {
		return nil, tooLarge()
	}
	if err != nil {
		return nil, s3MetadataReadError{cause: err}
	}
	if err := validateS3WriteResult(r.Method, r.URL, r.Header, response.Header, response.StatusCode, data); err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusOK {
		if err := validateS3ObjectAttributes(r.Context(), r.Method, r.URL, data); err != nil {
			return nil, err
		}
	}
	if root := s3ListingRoot(r.Method, r.URL, response.StatusCode); root != "" {
		if err := validateS3Listing(data, root); err != nil {
			return nil, err
		}
	}
	response.Body = io.NopCloser(bytes.NewReader(data))
	return response, nil
}

// Only classify known list operations. Object GET bodies are never inspected,
// even if the object itself contains XML that looks like an S3 error.
func s3ListingRoot(method string, u *url.URL, status int) string {
	if method != http.MethodGet || status != http.StatusOK {
		return ""
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return ""
	}
	bucket, key, object := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	if bucket == "" && len(q) == 0 {
		return "ListAllMyBucketsResult"
	}
	if object && key != "" {
		if q.Has("uploadId") {
			return "ListPartsResult"
		}
		return ""
	}
	if q.Has("uploads") {
		return "ListMultipartUploadsResult"
	}
	// Other bucket subresources have their own decoding contracts.
	for name := range q {
		switch name {
		case "list-type", "prefix", "delimiter", "marker", "max-keys", "continuation-token", "start-after", "encoding-type", "fetch-owner", "metadata", "unsorted":
		default:
			return ""
		}
	}
	return "ListBucketResult"
}

func validateS3Listing(data []byte, expected string) error {
	invalid := func() error {
		return apperror.New(502, "InvalidS3ListingResponse", messages.StorageInvalidS3ListingResponse)
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	depth, seen := 0, false
	for {
		token, err := d.Token()
		if err == io.EOF {
			if !seen || depth != 0 {
				return invalid()
			}
			return nil
		}
		if err != nil {
			return invalid()
		}
		switch token := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				if seen || token.Name.Local != expected {
					return invalid()
				}
				seen = true
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(token)) != "" {
				return invalid()
			}
		}
	}
}

package storage

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
)

// ScopeTransport checks the actual signed upstream resources, including copy
// sources and every key of a multi-delete. It never rewrites signed requests.
type ScopeTransport struct {
	Base   http.RoundTripper
	Policy authstate.Policy
	Load   func(context.Context) (authstate.Policy, error)
	// CheckCleanup fences native credential replacement without requiring the
	// access itself to remain active for cancellation cleanup.
	CheckCleanup func(context.Context) error
	Admin        bool
}

func (t ScopeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := func() (*http.Response, error) {
		p := t.Policy
		// An SDK cleanup may abort an upload created before expiry/revocation. No data
		// read, write or arbitrary delete is allowed through this cleanup exception.
		bucket, key := splitS3Path(r.URL.Path)
		if !t.Admin && r.Method == "DELETE" && r.URL.Query().Get("uploadId") != "" && p.Allows("objects:write") && p.AllowsObject(bucket, key) {
			if t.CheckCleanup != nil {
				if err := t.CheckCleanup(r.Context()); err != nil {
					return nil, err
				}
			}
			return t.Base.RoundTrip(r)
		}
		if t.Load != nil {
			var err error
			p, err = t.Load(r.Context())
			if err != nil {
				return nil, err
			}
		}
		if p.Full() {
			return t.Base.RoundTrip(r)
		}
		if t.Admin {
			if b := r.URL.Query().Get("bucket"); b != "" {
				scope := "buckets:read"
				if r.Method != "GET" && r.Method != "HEAD" {
					scope = "buckets:manage"
				}
				if !p.Allows(scope) || !p.AllowsBucket(b) {
					return nil, authstate.Denied()
				}
				return t.Base.RoundTrip(r)
			}
			scope := "server:read"
			path := r.URL.Path
			if strings.Contains(path, "accountinfo") {
				return nil, authstate.Denied()
			}
			for _, part := range []string{"user", "group", "policy", "service-account", "accesskey", "access-key", "sts"} {
				if strings.Contains(path, part) {
					scope = "iam:read"
					break
				}
			}
			read := r.Method == "GET" || r.Method == "HEAD" || r.Method == "POST" && strings.HasSuffix(path, "/list-access-keys")
			if !read {
				scope = strings.TrimSuffix(scope, ":read") + ":manage"
			}
			if !p.Allows(scope) {
				return nil, authstate.Denied()
			}
			return t.Base.RoundTrip(r)
		}
		q := r.URL.Query()
		op := scopeOperation{copySource: r.Header.Get("X-Amz-Copy-Source"), prefix: q.Get("prefix"), multipart: q.Get("uploadId") != "", selectQuery: q.Has("select"), multiDelete: q.Has("delete"), location: q.Has("location")}
		for _, name := range []string{"versioning", "encryption", "cors", "lifecycle", "policy", "tagging", "notification", "replication", "object-lock", "acl", "logging", "website", "quota"} {
			if q.Has(name) {
				op.bucketConfig = true
				break
			}
		}
		if r.Method == "POST" && key == "" && op.multiDelete {
			if r.Body == nil || !p.Allows("objects:delete") {
				return nil, authstate.Denied()
			}
			raw, err := io.ReadAll(io.LimitReader(r.Body, 2*1024*1024+1))
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(raw))
			if err != nil || len(raw) > 2*1024*1024 {
				return nil, authstate.Denied()
			}
			op.deleteKeys, err = scopeDeleteKeys(raw)
			if err != nil {
				return nil, err
			}
		}
		if err := checkS3Scope(p, r.Method, bucket, key, op); err != nil {
			return nil, err
		}
		return t.Base.RoundTrip(r)
	}()
	if err == nil || apperror.Public(err).Code != "AccessScopeDenied" {
		return response, err
	}
	// SDKs retry transport failures. A local authorization refusal is an ordinary
	// 403 response, so it must not become a retry loop or a timeout.
	if r.Body != nil {
		_ = r.Body.Close()
	}
	raw := `<Error><Code>AccessDenied</Code><Message>Access denied</Message></Error>`
	media := "application/xml"
	if t.Admin {
		raw = `{"Code":"AccessDenied","Message":"Access denied"}`
		media = "application/json"
	}
	return &http.Response{StatusCode: 403, Status: "403 Forbidden", Request: r, Header: http.Header{"Content-Type": {media}, "Content-Length": {strconv.Itoa(len(raw))}}, ContentLength: int64(len(raw)), Body: io.NopCloser(strings.NewReader(raw))}, nil
}

func splitS3Path(path string) (string, string) {
	bucket, key, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	return bucket, key
}

type scopeOperation struct {
	copySource, prefix                                          string
	multipart, selectQuery, multiDelete, location, bucketConfig bool
	deleteKeys                                                  []string
}

func checkS3Scope(p authstate.Policy, method, bucket, key string, op scopeOperation) error {
	if source := op.copySource; source != "" {
		path, _, _ := strings.Cut(source, "?")
		decoded, err := url.PathUnescape(path)
		if err != nil {
			return authstate.Denied()
		}
		b, k := splitS3Path(decoded)
		if !p.Allows("objects:read") || !p.AllowsObject(b, k) {
			return authstate.Denied()
		}
	}
	if bucket == "" {
		if method == "GET" && p.Allows("buckets:read") {
			return nil
		}
		return authstate.Denied()
	}
	if key != "" {
		if !p.AllowsObject(bucket, key) {
			return authstate.Denied()
		}
		scope := "objects:read"
		switch method {
		case "HEAD":
			if p.Allows("objects:read") || p.Allows("objects:write") || p.Allows("objects:delete") {
				return nil
			}
			return authstate.Denied()
		case "PUT":
			scope = "objects:write"
		case "DELETE":
			scope = "objects:delete"
			if op.multipart {
				scope = "objects:write"
			}
		case "POST":
			if !op.selectQuery {
				scope = "objects:write"
			}
		case "GET":
			if op.multipart {
				scope = "objects:write"
			}
		default:
			return authstate.Denied()
		}
		if !p.Allows(scope) {
			return authstate.Denied()
		}
		return nil
	}
	if method == "POST" && op.multiDelete {
		if !p.Allows("objects:delete") {
			return authstate.Denied()
		}
		for _, key := range op.deleteKeys {
			if !p.AllowsObject(bucket, key) {
				return authstate.Denied()
			}
		}
		return nil
	}
	if op.location || method == "HEAD" {
		if p.ListsBucket(bucket) && (p.Allows("buckets:read") || p.Allows("objects:read") || p.Allows("objects:write") || p.Allows("objects:delete")) {
			return nil
		}
		return authstate.Denied()
	}
	if method == "GET" && !op.bucketConfig {
		if p.Allows("objects:read") && p.AllowsObject(bucket, op.prefix) {
			return nil
		}
		return authstate.Denied()
	}
	scope := "buckets:read"
	if method != "GET" && method != "HEAD" {
		scope = "buckets:manage"
	}
	if p.Allows(scope) && p.AllowsBucket(bucket) {
		return nil
	}
	return authstate.Denied()
}

func scopeDeleteKeys(raw []byte) ([]string, error) {
	var value struct {
		XMLName xml.Name `xml:"Delete"`
		Objects []struct {
			Key string `xml:"Key"`
		} `xml:"Object"`
	}
	if xml.Unmarshal(raw, &value) != nil || len(value.Objects) == 0 || len(value.Objects) > 1000 {
		return nil, authstate.Denied()
	}
	keys := make([]string, 0, len(value.Objects))
	for _, o := range value.Objects {
		keys = append(keys, o.Key)
	}
	return keys, nil
}

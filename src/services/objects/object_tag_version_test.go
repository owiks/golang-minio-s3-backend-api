package objects

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/tags"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

type tagVersionClient struct {
	UpdatePropertiesClient
	info              minio.ObjectInfo
	values            map[string]map[string]string
	reads, writes     []string
	heads             int
	headErrors        map[int]error
	readErr, writeErr error
	onRead, onWrite   func()
	bodyClient        *minio.Client
	bodyOptions       minio.GetObjectOptions
	putOptions        minio.PutObjectOptions
	putCalls          int
}

func newTagVersionClient(version string) *tagVersionClient {
	return &tagVersionClient{info: minio.ObjectInfo{ETag: "first", Size: 3, LastModified: time.Unix(10, 0), VersionID: version, ContentType: "text/plain", Metadata: make(http.Header)}, values: map[string]map[string]string{version: {"owner": "old"}, "v2": {"owner": "new"}}, headErrors: map[int]error{}}
}

func (c *tagVersionClient) StatObject(ctx context.Context, _, _ string, _ minio.StatObjectOptions) (minio.ObjectInfo, error) {
	c.heads++
	if err := ctx.Err(); err != nil {
		return minio.ObjectInfo{}, err
	}
	if err := c.headErrors[c.heads]; err != nil {
		return minio.ObjectInfo{}, err
	}
	return c.info, nil
}
func (c *tagVersionClient) GetObjectTagging(ctx context.Context, _, _ string, o minio.GetObjectTaggingOptions) (*tags.Tags, error) {
	c.reads = append(c.reads, o.VersionID)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.onRead != nil {
		f := c.onRead
		c.onRead = nil
		f()
	}
	if c.readErr != nil {
		return nil, c.readErr
	}
	version := o.VersionID
	if version == "" {
		version = c.info.VersionID
	}
	return tags.NewTags(c.values[version], true)
}
func (c *tagVersionClient) PutObjectTagging(ctx context.Context, _, _ string, t *tags.Tags, o minio.PutObjectTaggingOptions) error {
	c.writes = append(c.writes, o.VersionID)
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.onWrite != nil {
		f := c.onWrite
		c.onWrite = nil
		f()
	}
	if c.writeErr != nil {
		return c.writeErr
	}
	version := o.VersionID
	if version == "" {
		version = c.info.VersionID
	}
	c.values[version] = t.ToMap()
	return nil
}
func (c *tagVersionClient) GetObject(ctx context.Context, b, k string, o minio.GetObjectOptions) (*minio.Object, error) {
	c.bodyOptions = o
	if c.bodyClient == nil {
		panic("unexpected body read")
	}
	return c.bodyClient.GetObject(ctx, b, k, o)
}
func (c *tagVersionClient) PutObject(ctx context.Context, _, _ string, r io.Reader, _ int64, o minio.PutObjectOptions) (minio.UploadInfo, error) {
	c.putCalls++
	c.putOptions = o
	if err := ctx.Err(); err != nil {
		return minio.UploadInfo{}, err
	}
	if strings.Trim(o.Header().Get("If-Match"), `"`) != c.info.ETag {
		return minio.UploadInfo{}, minio.ErrorResponse{Code: "PreconditionFailed"}
	}
	_, err := io.ReadAll(r)
	return minio.UploadInfo{ETag: "written"}, err
}

func tagSnapshot(t *testing.T, c *tagVersionClient) ObjectProperties {
	t.Helper()
	_, p, err := ReadProperties(context.Background(), c, "bucket", "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	c.heads = 0
	c.reads = nil
	c.writes = nil
	return p
}

func TestVersionedTagsStayWithSnapshotAcrossReplacementOrDeleteMarker(t *testing.T) {
	for _, timing := range []string{"before-tag-read", "before-tag-write", "delete-marker"} {
		t.Run(timing, func(t *testing.T) {
			c := newTagVersionClient("v1")
			before := tagSnapshot(t, c)
			replace := func() {
				c.info.VersionID = "v2"
				c.info.ETag = "second"
				if timing == "delete-marker" {
					c.headErrors[2] = minio.ErrorResponse{Code: "NoSuchKey"}
				}
			}
			if timing == "before-tag-write" {
				c.onWrite = replace
			} else {
				c.onRead = replace
			}
			result, err := UpdateProperties(context.Background(), c, "bucket", "file.txt", ObjectPropertiesRequest{Revision: before.Revision, Tags: map[string]string{"env": "test"}, TagsMode: "merge"})
			if err != nil || !result.OK || c.heads != 1 || !reflect.DeepEqual(c.reads, []string{"v1"}) || !reflect.DeepEqual(c.writes, []string{"v1"}) {
				t.Fatal("target changed or fallback used", result, err, c.reads, c.writes, c.heads)
			}
			if !reflect.DeepEqual(c.values["v1"], map[string]string{"owner": "old", "env": "test"}) || !reflect.DeepEqual(c.values["v2"], map[string]string{"owner": "new"}) {
				t.Fatal("other version modified", c.values)
			}
		})
	}
}

func TestVersionedPropertiesReadDoesNotMixCurrentTags(t *testing.T) {
	c := newTagVersionClient("v1")
	c.onRead = func() { c.info.VersionID = "v2"; c.info.ETag = "second" }
	info, p, err := ReadProperties(context.Background(), c, "bucket", "file.txt")
	if err != nil || info.VersionID != "v1" || p.Version != "v1" || p.Tags["owner"] != "old" || c.heads != 1 || !reflect.DeepEqual(c.reads, []string{"v1"}) {
		t.Fatal(info, p, err, c.reads)
	}
}

func TestPinnedTagPermissionsAndMissingVersionNeverFallBack(t *testing.T) {
	for _, code := range []string{"AccessDenied", "NoSuchVersion"} {
		t.Run(code, func(t *testing.T) {
			c := newTagVersionClient("v1")
			c.readErr = minio.ErrorResponse{Code: code}
			before := tagSnapshot(t, c)
			if messages.Empty(before.TagsError) || before.Version != "v1" {
				t.Fatal("unavailable tags hidden", before)
			}
			_, err := UpdateProperties(context.Background(), c, "bucket", "file.txt", ObjectPropertiesRequest{Revision: before.Revision, Tags: map[string]string{"bad": "write"}})
			if err == nil || apperror.Public(err).Code != "ObjectTagsUnavailable" || len(c.writes) != 0 || !reflect.DeepEqual(c.reads, []string{"v1"}) {
				t.Fatal("read fell back or write allowed", err, c.reads, c.writes)
			}
		})
	}
	for _, failure := range []error{minio.ErrorResponse{Code: "AccessDenied"}, minio.ErrorResponse{Code: "NoSuchVersion"}, context.DeadlineExceeded} {
		t.Run(failure.Error(), func(t *testing.T) {
			c := newTagVersionClient("v1")
			before := tagSnapshot(t, c)
			c.writeErr = failure
			result, err := UpdateProperties(context.Background(), c, "bucket", "file.txt", ObjectPropertiesRequest{Revision: before.Revision, Tags: map[string]string{}})
			missing := minio.ToErrorResponse(failure).Code == "NoSuchVersion"
			if result.OK || err == nil || missing && apperror.Public(err).Code != "ObjectPropertiesChanged" || !missing && !errors.Is(err, failure) || !reflect.DeepEqual(c.writes, []string{"v1"}) {
				t.Fatal("write retried or error lost", result, err, c.writes)
			}
		})
	}
}

func TestMutableTagTargetsDetectChangesWithoutClaimingAtomicity(t *testing.T) {
	for _, version := range []string{"", "null"} {
		for _, phase := range []string{"stable", "during-read", "during-write", "deleted-during-read", "deleted-after-write", "verify-denied", "verify-cancelled"} {
			t.Run(version+"/"+phase, func(t *testing.T) {
				c := newTagVersionClient(version)
				before := tagSnapshot(t, c)
				replace := func() { c.info.ETag = "second"; c.values[version] = map[string]string{"other": "new"} }
				switch phase {
				case "during-read":
					c.onRead = replace
				case "during-write":
					c.onWrite = replace
				case "deleted-during-read":
					c.headErrors[2] = minio.ErrorResponse{Code: "NoSuchKey"}
				case "deleted-after-write":
					c.headErrors[3] = minio.ErrorResponse{Code: "NoSuchKey"}
				case "verify-denied":
					c.headErrors[3] = minio.ErrorResponse{Code: "AccessDenied"}
				case "verify-cancelled":
					c.headErrors[3] = context.Canceled
				}
				result, err := UpdateProperties(context.Background(), c, "bucket", "file.txt", ObjectPropertiesRequest{Revision: before.Revision, Tags: map[string]string{}, TagsMode: "replace"})
				if phase == "stable" {
					if err != nil || !result.OK || c.heads != 3 || !reflect.DeepEqual(c.writes, []string{version}) || len(c.values[version]) != 0 {
						t.Fatal(result, err, c.heads, c.writes)
					}
					return
				}
				want := "ObjectPropertiesChanged"
				if phase == "verify-denied" {
					want = "ObjectTagsUnverified"
				}
				if phase == "verify-cancelled" {
					want = "Timeout"
				}
				if err == nil || result.OK || apperror.Public(err).Code != want {
					t.Fatal("race or unknown write reported as success", result, err)
				}
				wantWrites := 1
				if phase == "during-read" || phase == "deleted-during-read" {
					wantWrites = 0
				}
				if len(c.writes) != wantWrites {
					t.Fatal("unexpected write count", c.writes)
				}
				if phase == "verify-cancelled" && !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation lost", err)
				}
			})
		}
	}
}

func TestPinnedTagPatchModesAndStaleRevision(t *testing.T) {
	for _, mode := range []string{"merge", "replace", "remove"} {
		t.Run(mode, func(t *testing.T) {
			c := newTagVersionClient("v1")
			before := tagSnapshot(t, c)
			patch := map[string]string{"env": "test"}
			want := map[string]string{"env": "test"}
			if mode == "merge" {
				want["owner"] = "old"
			}
			if mode == "remove" {
				patch = map[string]string{"owner": ""}
				want = map[string]string{}
			}
			_, err := UpdateProperties(context.Background(), c, "bucket", "file.txt", ObjectPropertiesRequest{Revision: before.Revision, Tags: patch, TagsMode: mode})
			if err != nil || !reflect.DeepEqual(c.values["v1"], want) {
				t.Fatal(mode, c.values, err)
			}
		})
	}
	c := newTagVersionClient("v1")
	before := tagSnapshot(t, c)
	c.values["v1"] = map[string]string{"changed": "externally"}
	_, err := UpdateProperties(context.Background(), c, "bucket", "file.txt", ObjectPropertiesRequest{Revision: before.Revision, Tags: map[string]string{}})
	if err == nil || apperror.Public(err).Code != "ObjectPropertiesChanged" || len(c.writes) != 0 {
		t.Fatal("stale revision accepted", err, c.writes)
	}
}

type tagBodyTransport func(*http.Request) (*http.Response, error)

func (f tagBodyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEditorReadsBodyFromSameVersionAsMetadataAndTags(t *testing.T) {
	c := newTagVersionClient("v1")
	c.onRead = func() { c.info.VersionID = "v2"; c.info.ETag = "second" }
	var err error
	c.bodyClient, err = minio.New("fixture.invalid", &minio.Options{Creds: credentials.NewStaticV4("fixture", "fixture-secret", ""), Region: "us-east-1", MaxRetries: 1, Transport: tagBodyTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Query().Get("versionId") != "v1" || r.Header.Get("If-Match") != `"first"` {
			t.Error("body request not pinned")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Length": {"3"}, "Content-Type": {"text/plain"}, "Etag": {`"first"`}, "Last-Modified": {time.Unix(10, 0).UTC().Format(http.TimeFormat)}, "X-Amz-Version-Id": {"v1"}}, ContentLength: 3, Body: io.NopCloser(strings.NewReader("old"))}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	value, err := GetContent(context.Background(), c, "bucket", "file.txt")
	if err != nil || value.Text != "old" || !value.Writable || c.bodyOptions.VersionID != "v1" || !reflect.DeepEqual(c.reads, []string{"v1"}) {
		t.Fatal(value, err, c.bodyOptions, c.reads)
	}
}

func TestEditorPreservesPinnedTagsAndConditionalWrite(t *testing.T) {
	for _, race := range []bool{false, true} {
		t.Run(map[bool]string{false: "stable", true: "replacement"}[race], func(t *testing.T) {
			c := newTagVersionClient("v1")
			before := tagSnapshot(t, c)
			if race {
				c.onRead = func() { c.info.VersionID = "v2"; c.info.ETag = "second" }
			}
			result, err := UpdateContent(context.Background(), c, "bucket", "file.txt", ObjectContentRequest{Revision: before.Revision, Text: "changed"})
			if c.putCalls != 1 || !reflect.DeepEqual(c.putOptions.UserTags, map[string]string{"owner": "old"}) || c.putOptions.Header().Get("If-Match") != `"first"` {
				t.Fatal("tags mixed or destination condition lost", c.putCalls, c.putOptions.UserTags)
			}
			if race {
				if err == nil || result.OK || apperror.Public(err).Code != "DestinationChanged" {
					t.Fatal(result, err)
				}
			} else if err != nil || !result.OK {
				t.Fatal(result, err)
			}
		})
	}
}

func TestMutablePropertyReadChecksEntireTargetIdentity(t *testing.T) {
	for name, change := range map[string]func(*minio.ObjectInfo){
		"etag":     func(v *minio.ObjectInfo) { v.ETag = "other" },
		"size":     func(v *minio.ObjectInfo) { v.Size++ },
		"modified": func(v *minio.ObjectInfo) { v.LastModified = v.LastModified.Add(time.Second) },
		"version":  func(v *minio.ObjectInfo) { v.VersionID = "v2" },
	} {
		t.Run(name, func(t *testing.T) {
			c := newTagVersionClient("")
			c.onRead = func() { change(&c.info) }
			_, value, err := ReadProperties(context.Background(), c, "bucket", "file.txt")
			if err == nil || apperror.Public(err).Code != "ObjectPropertiesChanged" || value.Revision != "" || len(c.writes) != 0 {
				t.Fatal("mixed mutable snapshot returned", value, err)
			}
		})
	}
}

func TestPinnedEditorReadFailureNeverFallsBackToLatest(t *testing.T) {
	for _, code := range []string{"NoSuchVersion", "AccessDenied"} {
		t.Run(code, func(t *testing.T) {
			c := newTagVersionClient("v1")
			calls := 0
			var err error
			c.bodyClient, err = minio.New("fixture.invalid", &minio.Options{Creds: credentials.NewStaticV4("fixture", "fixture-secret", ""), Region: "us-east-1", MaxRetries: 1, Transport: tagBodyTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Query().Get("versionId") != "v1" {
					t.Error("version omitted on error")
				}
				status := 404
				if code == "AccessDenied" {
					status = 403
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("<Error><Code>" + code + "</Code></Error>"))}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			value, err := GetContent(context.Background(), c, "bucket", "file.txt")
			want := "ObjectChanged"
			if code == "AccessDenied" {
				want = "AccessDenied"
			}
			if err == nil || apperror.Public(err).Code != want || calls != 1 || value.Text != "" || c.bodyOptions.VersionID != "v1" {
				t.Fatal("body read fell back or returned partial result", value, err, calls)
			}
		})
	}
}

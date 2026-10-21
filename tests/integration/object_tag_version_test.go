package integration

import (
	"context"
	"io"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/tags"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

// The hooks perform real competing S3 writes between the service's requests.
type propertyRaceClient struct {
	*storage.S3Client
	beforeRead, beforeWrite func()
	reads, writes, bodies   []string
	rewriteTags             map[string]string
	copySource              string
}

func (c *propertyRaceClient) GetObjectTagging(ctx context.Context, b, k string, o minio.GetObjectTaggingOptions) (*tags.Tags, error) {
	c.reads = append(c.reads, o.VersionID)
	if c.beforeRead != nil {
		f := c.beforeRead
		c.beforeRead = nil
		f()
	}
	return c.S3Client.GetObjectTagging(ctx, b, k, o)
}
func (c *propertyRaceClient) PutObjectTagging(ctx context.Context, b, k string, t *tags.Tags, o minio.PutObjectTaggingOptions) error {
	c.writes = append(c.writes, o.VersionID)
	if c.beforeWrite != nil {
		f := c.beforeWrite
		c.beforeWrite = nil
		f()
	}
	return c.S3Client.PutObjectTagging(ctx, b, k, t, o)
}
func (c *propertyRaceClient) GetObject(ctx context.Context, b, k string, o minio.GetObjectOptions) (*minio.Object, error) {
	c.bodies = append(c.bodies, o.VersionID)
	return c.S3Client.GetObject(ctx, b, k, o)
}
func (c *propertyRaceClient) NewMultipartUpload(ctx context.Context, b, k string, o minio.PutObjectOptions) (string, error) {
	c.rewriteTags = o.UserTags
	return c.S3Client.NewMultipartUpload(ctx, b, k, o)
}
func (c *propertyRaceClient) CopyObjectPart(ctx context.Context, sb, sk, db, dk, id string, n int, off, size int64, h map[string]string) (minio.CompletePart, error) {
	c.copySource = h["x-amz-copy-source"]
	return c.S3Client.CopyObjectPart(ctx, sb, sk, db, dk, id, n, off, size, h)
}

func putTaggedVersion(t *testing.T, s *minio.Client, b, k, body, owner string) minio.UploadInfo {
	t.Helper()
	v, err := s.PutObject(context.Background(), b, k, strings.NewReader(body), int64(len(body)), minio.PutObjectOptions{ContentType: "text/plain", CacheControl: "private", UserMetadata: map[string]string{"keep": owner}, UserTags: map[string]string{"owner": owner}})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func nativeVersionTags(t *testing.T, s *minio.Client, b, k, version string) map[string]string {
	t.Helper()
	v, err := s.GetObjectTagging(context.Background(), b, k, minio.GetObjectTaggingOptions{VersionID: version})
	if err != nil {
		t.Fatal(err)
	}
	return v.ToMap()
}
func nativeVersionBody(t *testing.T, s *minio.Client, b, k, version string) string {
	t.Helper()
	o, err := s.GetObject(context.Background(), b, k, minio.GetObjectOptions{VersionID: version})
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	data, err := io.ReadAll(o)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func checkedProperties(t *testing.T, c *propertyRaceClient, b, k string) objects.ObjectProperties {
	t.Helper()
	p, err := objects.GetProperties(context.Background(), c, b, k)
	if err != nil {
		t.Fatal(err)
	}
	c.reads = nil
	c.writes = nil
	c.bodies = nil
	return p
}

func TestPinnedObjectTagsNativeIntegration(t *testing.T) {
	_, s, b := fileFixture(t)
	ctx := context.Background()
	if err := s.SetBucketVersioning(ctx, b, minio.BucketVersioningConfiguration{Status: minio.Enabled}); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"before-read", "before-write", "delete-marker", "removed-version", "removed-before-write"} {
		t.Run(phase, func(t *testing.T) {
			key := phase + ".txt"
			old := putTaggedVersion(t, s, b, key, "old body", "old")
			c := &propertyRaceClient{S3Client: storage.NewS3Client(s)}
			before := checkedProperties(t, c, b, key)
			if old.VersionID == "" || before.Version != old.VersionID {
				t.Fatal("fixture not versioned", old, before)
			}
			var newer minio.UploadInfo
			replace := func() { newer = putTaggedVersion(t, s, b, key, "different new body", "new") }
			switch phase {
			case "before-read":
				c.beforeRead = replace
			case "before-write":
				c.beforeWrite = replace
			case "delete-marker":
				c.beforeWrite = func() {
					if err := s.RemoveObject(ctx, b, key, minio.RemoveObjectOptions{}); err != nil {
						t.Fatal(err)
					}
				}
			case "removed-version":
				c.beforeRead = func() {
					replace()
					if err := s.RemoveObject(ctx, b, key, minio.RemoveObjectOptions{VersionID: old.VersionID}); err != nil {
						t.Fatal(err)
					}
				}
			case "removed-before-write":
				c.beforeWrite = func() {
					replace()
					if err := s.RemoveObject(ctx, b, key, minio.RemoveObjectOptions{VersionID: old.VersionID}); err != nil {
						t.Fatal(err)
					}
				}
			}
			result, err := objects.UpdateProperties(ctx, c, b, key, objects.ObjectPropertiesRequest{Revision: before.Revision, Tags: map[string]string{"env": "test"}, TagsMode: "merge"})
			if !reflect.DeepEqual(c.reads, []string{old.VersionID}) {
				t.Fatal("tag read not pinned", c.reads)
			}
			if phase == "removed-version" || phase == "removed-before-write" {
				wantWrites := []string(nil)
				if phase == "removed-before-write" {
					wantWrites = []string{old.VersionID}
				}
				if err == nil || result.OK || !reflect.DeepEqual(c.writes, wantWrites) || apperror.Public(err).Code != "ObjectPropertiesChanged" {
					t.Fatal("missing version fell back", result, err, c.writes)
				}
			} else {
				if err != nil || !result.OK || !reflect.DeepEqual(c.writes, []string{old.VersionID}) {
					t.Fatal(result, err, c.writes)
				}
				if actual := nativeVersionTags(t, s, b, key, old.VersionID); !reflect.DeepEqual(actual, map[string]string{"owner": "old", "env": "test"}) {
					t.Fatal(actual)
				}
			}
			if phase == "delete-marker" {
				if _, err := s.StatObject(ctx, b, key, minio.StatObjectOptions{}); minio.ToErrorResponse(err).Code != "NoSuchKey" {
					t.Fatal("tag write resurrected a deleted key", err)
				}
			} else if actual := nativeVersionTags(t, s, b, key, newer.VersionID); !reflect.DeepEqual(actual, map[string]string{"owner": "new"}) || nativeVersionBody(t, s, b, key, "") != "different new body" {
				t.Fatal("replacement changed", actual)
			}
		})
	}
}

func TestPropertyEditorAndRewritePinnedVersionNativeIntegration(t *testing.T) {
	_, s, b := fileFixture(t)
	ctx := context.Background()
	if err := s.SetBucketVersioning(ctx, b, minio.BucketVersioningConfiguration{Status: minio.Enabled}); err != nil {
		t.Fatal(err)
	}
	t.Run("editor-read", func(t *testing.T) {
		key := "editor-read.txt"
		old := putTaggedVersion(t, s, b, key, "old body", "old")
		c := &propertyRaceClient{S3Client: storage.NewS3Client(s)}
		c.beforeRead = func() { putTaggedVersion(t, s, b, key, "new body with different properties", "new") }
		value, err := objects.GetContent(ctx, c, b, key)
		if err != nil || value.Text != "old body" || !value.Writable || !reflect.DeepEqual(c.reads, []string{old.VersionID}) || !reflect.DeepEqual(c.bodies, []string{old.VersionID}) {
			t.Fatal("mixed editor snapshot", value, err, c.reads, c.bodies)
		}
	})
	for _, operation := range []string{"editor-write", "metadata-rewrite"} {
		for _, race := range []bool{false, true} {
			t.Run(operation+map[bool]string{false: "/stable", true: "/race"}[race], func(t *testing.T) {
				key := operation + map[bool]string{false: "-stable.txt", true: "-race.txt"}[race]
				old := putTaggedVersion(t, s, b, key, "old body", "old")
				c := &propertyRaceClient{S3Client: storage.NewS3Client(s)}
				before := checkedProperties(t, c, b, key)
				if race {
					c.beforeRead = func() { putTaggedVersion(t, s, b, key, "new body with different properties", "new") }
				}
				var err error
				if operation == "editor-write" {
					_, err = objects.UpdateContent(ctx, c, b, key, objects.ObjectContentRequest{Revision: before.Revision, Text: "edited body"})
				} else {
					_, err = objects.UpdateProperties(ctx, c, b, key, objects.ObjectPropertiesRequest{Revision: before.Revision, Headers: map[string]string{"Cache-Control": "max-age=60"}})
				}
				if !reflect.DeepEqual(c.reads, []string{old.VersionID}) {
					t.Fatal("preserved tags not pinned", c.reads)
				}
				if operation == "metadata-rewrite" && (!reflect.DeepEqual(c.rewriteTags, map[string]string{"owner": "old"}) || (c.copySource != "" || !race) && !strings.HasSuffix(c.copySource, "?versionId="+url.QueryEscape(old.VersionID))) {
					t.Fatal("rewrite source or tags mixed", c.rewriteTags, c.copySource)
				}
				if race {
					if err == nil || apperror.Public(err).Code != "DestinationChanged" {
						t.Fatal("concurrent destination overwritten", err)
					}
					if actual := nativeVersionTags(t, s, b, key, ""); !reflect.DeepEqual(actual, map[string]string{"owner": "new"}) || nativeVersionBody(t, s, b, key, "") != "new body with different properties" {
						t.Fatal(actual)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					if actual := nativeVersionTags(t, s, b, key, ""); !reflect.DeepEqual(actual, map[string]string{"owner": "old"}) {
						t.Fatal("tags not preserved", actual)
					}
					want := "old body"
					if operation == "editor-write" {
						want = "edited body"
					}
					if actual := nativeVersionBody(t, s, b, key, ""); actual != want {
						t.Fatal(actual)
					}
					info, e := s.StatObject(ctx, b, key, minio.StatObjectOptions{})
					if e != nil || info.UserMetadata["Keep"] != "old" && info.UserMetadata["keep"] != "old" {
						t.Fatal("metadata lost", info, e)
					}
				}
				if actual := nativeVersionTags(t, s, b, key, old.VersionID); !reflect.DeepEqual(actual, map[string]string{"owner": "old"}) {
					t.Fatal("old version changed", actual)
				}
			})
		}
	}
}

func TestMutableObjectTagsNativeIntegration(t *testing.T) {
	for _, mode := range []string{"unversioned", "null"} {
		t.Run(mode, func(t *testing.T) {
			_, s, b := fileFixture(t)
			ctx := context.Background()
			if mode == "null" {
				if err := s.SetBucketVersioning(ctx, b, minio.BucketVersioningConfiguration{Status: minio.Enabled}); err != nil {
					t.Fatal(err)
				}
				if err := s.SetBucketVersioning(ctx, b, minio.BucketVersioningConfiguration{Status: minio.Suspended}); err != nil {
					t.Fatal(err)
				}
			}
			for _, phase := range []string{"stable", "before-read", "before-write"} {
				t.Run(phase, func(t *testing.T) {
					key := phase + ".txt"
					putTaggedVersion(t, s, b, key, "old body", "old")
					c := &propertyRaceClient{S3Client: storage.NewS3Client(s)}
					before := checkedProperties(t, c, b, key)
					wantVersion := before.Version
					if wantVersion != "" && wantVersion != "null" || mode == "unversioned" && wantVersion != "" {
						t.Fatal("unexpected fixture version", before.Version)
					}
					if mode == "null" {
						found := false
						for object := range s.ListObjects(ctx, b, minio.ListObjectsOptions{Prefix: key, WithVersions: true}) {
							if object.Err != nil {
								t.Fatal(object.Err)
							}
							if object.Key == key && object.VersionID == "null" {
								found = true
							}
						}
						if !found {
							t.Fatal("fixture has no physical null version")
						}
					}
					replace := func() { putTaggedVersion(t, s, b, key, "new body", "new") }
					if phase == "before-read" {
						c.beforeRead = replace
					}
					if phase == "before-write" {
						c.beforeWrite = replace
					}
					result, err := objects.UpdateProperties(ctx, c, b, key, objects.ObjectPropertiesRequest{Revision: before.Revision, Tags: map[string]string{"env": "test"}, TagsMode: "merge"})
					if phase == "stable" {
						if err != nil || !result.OK {
							t.Fatal(result, err)
						}
					} else if err == nil || result.OK || apperror.Public(err).Code != "ObjectPropertiesChanged" {
						t.Fatal("mutable race reported as success", result, err)
					}
					if !reflect.DeepEqual(c.reads, []string{wantVersion}) {
						t.Fatal("null/empty read target changed", c.reads)
					}
					if phase == "before-read" {
						if len(c.writes) != 0 || nativeVersionTags(t, s, b, key, wantVersion)["owner"] != "new" {
							t.Fatal("race not stopped before write", c.writes)
						}
					} else if !reflect.DeepEqual(c.writes, []string{wantVersion}) {
						t.Fatal("write target changed", c.writes)
					}
					// The during-write race may affect the mutable slot: the service
					// must report conflict rather than claim tagging was atomic.
				})
			}
		})
	}
}

package objects

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

type attributeClientFunc func(context.Context, string, string, storage.ObjectAttributesOptions) (storage.ObjectAttributesPage, error)

func (f attributeClientFunc) ReadObjectAttributes(ctx context.Context, b, k string, o storage.ObjectAttributesOptions) (storage.ObjectAttributesPage, error) {
	return f(ctx, b, k, o)
}
func attributeTestPage(version string) storage.ObjectAttributesPage {
	size := int64(10)
	count := 2
	truncated := true
	next := 1
	return storage.ObjectAttributesPage{VersionID: version, ETag: "etag", Size: &size, PartsCount: &count, LastModified: time.Unix(10, 0).UTC(), Truncated: &truncated, NextPartNumberMarker: &next, Parts: []storage.ObjectAttributePart{{Number: 1, Size: 5}}}
}
func TestAttributePaginationRequiresMutableSnapshot(t *testing.T) {
	for _, version := range []string{"", "null"} {
		t.Run("version="+version, func(t *testing.T) {
			calls := 0
			client := attributeClientFunc(func(ctx context.Context, b, k string, o storage.ObjectAttributesOptions) (storage.ObjectAttributesPage, error) {
				calls++
				if b != "bucket" || k != "file" || o.VersionID != version {
					t.Fatal(b, k, o)
				}
				return attributeTestPage(version), nil
			})
			input := ObjectAttributesInput{Bucket: "bucket", Key: "file", VersionID: version}
			first, err := GetAttributes(context.Background(), client, input)
			if err != nil || first.Consistency != "observed" || len(first.Snapshot) != 64 {
				t.Fatal(first, err)
			}
			input.PartNumberMarker = 1
			if _, err = GetAttributes(context.Background(), client, input); apperror.Public(err).Code != "AttributesSnapshotRequired" || calls != 1 {
				t.Fatal(err, calls)
			}
			input.Snapshot = first.Snapshot
			if _, err = GetAttributes(context.Background(), client, input); err != nil {
				t.Fatal(err)
			}
			changed := attributeClientFunc(func(context.Context, string, string, storage.ObjectAttributesOptions) (storage.ObjectAttributesPage, error) {
				page := attributeTestPage(version)
				page.ETag = "new"
				return page, nil
			})
			if _, err = GetAttributes(context.Background(), changed, input); apperror.Public(err).Code != "ObjectAttributesChanged" {
				t.Fatal(err)
			}
		})
	}
}
func TestAttributeConcreteVersionAndCurrentChange(t *testing.T) {
	calls := 0
	client := attributeClientFunc(func(ctx context.Context, b, k string, o storage.ObjectAttributesOptions) (storage.ObjectAttributesPage, error) {
		calls++
		if o.VersionID != "old-version" || o.PartNumberMarker != 1 || o.MaxParts != 1 {
			t.Fatal(o)
		}
		page := attributeTestPage(o.VersionID)
		value := false
		page.Truncated = &value
		page.NextPartNumberMarker = nil
		page.Parts = []storage.ObjectAttributePart{{Number: 2, Size: 5}}
		return page, nil
	})
	result, err := GetAttributes(context.Background(), client, ObjectAttributesInput{Bucket: "bucket", Key: "file", VersionID: "old-version", PartNumberMarker: 1, MaxParts: 1})
	if err != nil || result.VersionID != "old-version" || result.Consistency != "version" || calls != 1 {
		t.Fatal(result, err, calls)
	}
}
func TestAttributeInputRejectedBeforeUpstream(t *testing.T) {
	client := attributeClientFunc(func(context.Context, string, string, storage.ObjectAttributesOptions) (storage.ObjectAttributesPage, error) {
		t.Fatal("invalid input reached upstream")
		return storage.ObjectAttributesPage{}, nil
	})
	for _, input := range []ObjectAttributesInput{
		{Key: "file"}, {Bucket: "bucket", Key: "file", MaxParts: -1}, {Bucket: "bucket", Key: "file", MaxParts: 1001}, {Bucket: "bucket", Key: "file", PartNumberMarker: 10001}, {Bucket: "bucket", Key: "file", Snapshot: "bad"}, {Bucket: "bucket", Key: "file", VersionID: "x\n"},
	} {
		if _, err := GetAttributes(context.Background(), client, input); err == nil {
			t.Fatal("accepted", input)
		}
	}
}
func TestAttributeAbsentValuesStayAbsentInJSON(t *testing.T) {
	client := attributeClientFunc(func(context.Context, string, string, storage.ObjectAttributesOptions) (storage.ObjectAttributesPage, error) {
		return storage.ObjectAttributesPage{LastModified: time.Unix(10, 0)}, nil
	})
	result, err := GetAttributes(context.Background(), client, ObjectAttributesInput{Bucket: "bucket", Key: "file"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	for _, field := range []string{"\"size\"", "partsCount", "truncated", "checksumType"} {
		if strings.Contains(string(raw), field) {
			t.Fatal("invented absent field", string(raw))
		}
	}
	if !strings.Contains(string(raw), `"parts":null`) {
		t.Fatal(string(raw))
	}
}
func TestAttributeMutableContinuationWithoutIdentityFails(t *testing.T) {
	client := attributeClientFunc(func(context.Context, string, string, storage.ObjectAttributesOptions) (storage.ObjectAttributesPage, error) {
		p := attributeTestPage("")
		p.Size = nil
		return p, nil
	})
	_, err := GetAttributes(context.Background(), client, ObjectAttributesInput{Bucket: "bucket", Key: "file"})
	if apperror.Public(err).Code != "AttributesSnapshotUnavailable" {
		t.Fatal(err)
	}
}

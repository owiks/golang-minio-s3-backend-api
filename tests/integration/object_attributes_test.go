package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
)

func TestObjectAttributesNativeIntegration(t *testing.T) {
	client, s3, bucket := fileFixture(t)
	ctx := context.Background()
	if err := s3.SetBucketVersioning(ctx, bucket, minio.BucketVersioningConfiguration{Status: minio.Enabled}); err != nil {
		t.Fatal(err)
	}
	old, err := s3.PutObject(ctx, bucket, "plain.txt", strings.NewReader("old body"), 8, minio.PutObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s3.PutObject(ctx, bucket, "plain.txt", strings.NewReader("current body"), 12, minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	query := url.Values{"bucket": {bucket}, "key": {"plain.txt"}, "versionId": {old.VersionID}}
	var plain objects.ObjectAttributes
	if err = json.Unmarshal(client.call("GET", "objects/attributes?"+query.Encode(), nil, 200), &plain); err != nil {
		t.Fatal(err)
	}
	if plain.VersionID != old.VersionID || plain.Size == nil || *plain.Size != 8 || plain.Consistency != "version" || plain.ETag != old.ETag {
		t.Fatal("old version not preserved", plain)
	}
	query.Set("key", "missing.txt")
	query.Del("versionId")
	client.call("GET", "objects/attributes?"+query.Encode(), nil, 404)
	query.Set("key", "plain.txt")
	query.Set("versionId", "00000000-0000-0000-0000-000000000001")
	data := client.call("GET", "objects/attributes?"+query.Encode(), nil, 404)
	if !bytes.Contains(data, []byte("NoSuchVersion")) {
		t.Fatal("version absence became key absence", string(data))
	}
	data = bytes.Repeat([]byte("m"), 12<<20)
	multi, err := s3.PutObject(ctx, bucket, "multi.bin", bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{PartSize: 5 << 20, NumThreads: 1})
	if err != nil {
		t.Fatal(err)
	}
	query = url.Values{"bucket": {bucket}, "key": {"multi.bin"}, "versionId": {multi.VersionID}, "maxParts": {"1"}}
	var first objects.ObjectAttributes
	if err = json.Unmarshal(client.call("GET", "objects/attributes?"+query.Encode(), nil, 200), &first); err != nil {
		t.Fatal(err)
	}
	if first.Size == nil || *first.Size != int64(len(data)) {
		t.Fatal(first)
	}
	if first.PartsCount == nil || *first.PartsCount != 3 || len(first.Parts) != 1 || first.Truncated == nil || !*first.Truncated || first.NextPartNumberMarker == nil {
		t.Fatal("fixture did not return multipart attributes", first)
	}
	seen := map[int]bool{first.Parts[0].Number: true}
	page := first
	for page.Truncated != nil && *page.Truncated {
		query.Set("partNumberMarker", fmtSize(*page.NextPartNumberMarker))
		query.Set("snapshot", first.Snapshot)
		if err = json.Unmarshal(client.call("GET", "objects/attributes?"+query.Encode(), nil, 200), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Parts) != 1 || seen[page.Parts[0].Number] {
			t.Fatal("repeated or unbounded page", page)
		}
		seen[page.Parts[0].Number] = true
		if len(seen) > 3 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(seen) != 3 {
		t.Fatal("incomplete multipart attributes", seen)
	}
}

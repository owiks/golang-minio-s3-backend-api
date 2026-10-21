package integration

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/minio/minio-go/v7"

	runtimesvc "minio-web/api/src/services/runtime"
	"minio-web/api/tests/testsession"
)

func TestS3MetadataBoundsPreserveLargeNativeObjectStreams(t *testing.T) {
	_, _, bucket := fileFixture(t)
	client := runtimesvc.NewClients(testsession.Native(t))
	ctx := context.Background()
	// Deliberately larger than the metadata response limit and XML-labelled:
	// neither MIME nor an opaque object key may turn file data into metadata.
	payload := bytes.Repeat([]byte("native-stream-"), 512*1024)
	key := "tagging?policy/Файл + # %.xml"
	if _, err := client.PutObject(ctx, bucket, key, bytes.NewReader(payload), int64(len(payload)), minio.PutObjectOptions{ContentType: "application/xml", UserTags: map[string]string{"kind": "stream"}}); err != nil {
		t.Fatal(err)
	}
	for _, ranged := range []bool{false, true} {
		t.Run(map[bool]string{false: "full", true: "range"}[ranged], func(t *testing.T) {
			opts := minio.GetObjectOptions{}
			want := payload
			if ranged {
				if err := opts.SetRange(11, int64(len(payload)-18)); err != nil {
					t.Fatal(err)
				}
				want = payload[11 : len(payload)-17]
			}
			object, err := client.GetObject(ctx, bucket, key, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer object.Close()
			got, err := io.ReadAll(object)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatal("large object stream changed", len(got), len(want), err)
			}
		})
	}
	info, err := client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil || info.Size != int64(len(payload)) {
		t.Fatal("object HEAD rejected", info.Size, err)
	}
	tags, err := client.GetObjectTagging(ctx, bucket, key, minio.GetObjectTaggingOptions{})
	if err != nil || tags == nil || tags.ToMap()["kind"] != "stream" {
		t.Fatal("finite object metadata changed", tags, err)
	}
	page, err := client.ListObjectPage(ctx, bucket, "", "", "", 1000)
	if err != nil || len(page.Contents) != 1 || page.Contents[0].Key != key {
		t.Fatal("native listing changed", page, err)
	}
}

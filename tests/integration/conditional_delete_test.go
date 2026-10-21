package integration

import (
	"context"
	"github.com/minio/minio-go/v7"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMinioConditionalDeleteCompatibility(t *testing.T) {
	_, client, bucket := fileFixture(t)
	ctx := context.Background()
	_, err := client.PutObject(ctx, bucket, "probe.txt", strings.NewReader("probe"), 5, minio.PutObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	headers := http.Header{"If-Match": {`"non-matching-etag"`}}
	signed, err := client.PresignHeader(ctx, "DELETE", bucket, "probe.txt", time.Minute, nil, headers)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(ctx, "DELETE", signed.String(), nil)
	req.Header = headers
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal("fixture DELETE failed")
	}
	response.Body.Close()
	if response.StatusCode != 204 {
		t.Fatalf("fixture capability changed; review conditional-delete detection (status %d)", response.StatusCode)
	}
	if _, err = client.StatObject(ctx, bucket, "probe.txt", minio.StatObjectOptions{}); minio.ToErrorResponse(err).Code != "NoSuchKey" {
		t.Fatal("fixture behavior changed; review compatibility guard")
	}
}

package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"minio-web/api/src/services/apperror"
)

func multipartSignerFixture(t *testing.T) (*S3Client, *int) {
	t.Helper()
	endpoint, err := url.Parse("https://browser-s3.example.test:9443")
	if err != nil {
		t.Fatal(err)
	}
	calls := new(int)
	client, err := NewS3(Config{Endpoint: endpoint, Region: "us-east-1", Credentials: credentials.NewStaticV4("fixture-access", "fixture-secret", "fixture-token")}, roundTripFunc(func(*http.Request) (*http.Response, error) {
		*calls++
		return nil, errors.New("signing must not transfer bytes or probe permission")
	}))
	if err != nil {
		t.Fatal(err)
	}
	return NewS3Client(client), calls
}

func multipartSHA256(value string) string {
	digest := sha256.Sum256([]byte(value))
	return base64.StdEncoding.EncodeToString(digest[:])
}

func TestPresignMultipartPartBindsPublicTargetAndChecksum(t *testing.T) {
	client, calls := multipartSignerFixture(t)
	key := "папка/файл + # %.txt"
	uploadID := "opaque+/=upload?&value"
	signed, err := client.PresignMultipartPart(context.Background(), "fixture-bucket", key, uploadID, 4, multipartSHA256("fixture bytes"), 47*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	query := signed.Query()
	if signed.Scheme != "https" || signed.Host != "browser-s3.example.test:9443" || signed.Path != "/fixture-bucket/"+key || query.Get("uploadId") != uploadID || query.Get("partNumber") != "4" {
		t.Fatal("signature target changed")
	}
	if query.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" || query.Get("X-Amz-Expires") != "47" || query.Get("X-Amz-Security-Token") != "fixture-token" || query.Get("X-Amz-Credential") == "" || query.Get("X-Amz-Signature") == "" {
		t.Fatal("official authenticated signature missing")
	}
	if query.Get("X-Amz-SignedHeaders") != "host;x-amz-checksum-sha256" {
		t.Fatal("checksum is not mandatory, or a browser-forbidden header was signed", query.Get("X-Amz-SignedHeaders"))
	}
	if _, err := time.Parse("20060102T150405Z", query.Get("X-Amz-Date")); err != nil {
		t.Fatal("invalid signature timestamp", err)
	}
	if strings.Contains(signed.String(), "fixture-secret") || *calls != 0 {
		t.Fatal("secret disclosed or signature triggered upstream IO")
	}
}

func TestPresignMultipartPartValidatesBeforeSigning(t *testing.T) {
	for _, tc := range []struct {
		name, bucket, key, upload, checksum string
		part                                int
		expiry                              time.Duration
	}{
		{name: "empty bucket", key: "file", upload: "upload", part: 1, expiry: time.Minute, checksum: multipartSHA256("part")},
		{name: "empty key", bucket: "fixture-bucket", upload: "upload", part: 1, expiry: time.Minute, checksum: multipartSHA256("part")},
		{name: "empty upload", bucket: "fixture-bucket", key: "file", part: 1, expiry: time.Minute, checksum: multipartSHA256("part")},
		{name: "oversized upload", bucket: "fixture-bucket", key: "file", upload: strings.Repeat("x", 4097), part: 1, expiry: time.Minute, checksum: multipartSHA256("part")},
		{name: "newline upload", bucket: "fixture-bucket", key: "file", upload: "upload\r\n", part: 1, expiry: time.Minute, checksum: multipartSHA256("part")},
		{name: "zero part", bucket: "fixture-bucket", key: "file", upload: "upload", expiry: time.Minute, checksum: multipartSHA256("part")},
		{name: "too many parts", bucket: "fixture-bucket", key: "file", upload: "upload", part: 10001, expiry: time.Minute, checksum: multipartSHA256("part")},
		{name: "too short expiry", bucket: "fixture-bucket", key: "file", upload: "upload", part: 1, expiry: time.Second - 1, checksum: multipartSHA256("part")},
		{name: "too long expiry", bucket: "fixture-bucket", key: "file", upload: "upload", part: 1, expiry: 5*time.Minute + 1, checksum: multipartSHA256("part")},
		{name: "empty checksum", bucket: "fixture-bucket", key: "file", upload: "upload", part: 1, expiry: time.Minute},
		{name: "bad base64", bucket: "fixture-bucket", key: "file", upload: "upload", part: 1, expiry: time.Minute, checksum: "_url_not_standard_"},
		{name: "short checksum", bucket: "fixture-bucket", key: "file", upload: "upload", part: 1, expiry: time.Minute, checksum: base64.StdEncoding.EncodeToString([]byte("short"))},
		{name: "newline checksum", bucket: "fixture-bucket", key: "file", upload: "upload", part: 1, expiry: time.Minute, checksum: multipartSHA256("part") + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, calls := multipartSignerFixture(t)
			signed, err := client.PresignMultipartPart(context.Background(), tc.bucket, tc.key, tc.upload, tc.part, tc.checksum, tc.expiry)
			if signed != nil || err == nil || apperror.Public(err).Code != "InvalidMultipartPart" || *calls != 0 {
				t.Fatal("invalid signing parameters accepted or contacted S3")
			}
		})
	}
}

func TestPresignMultipartPartHonorsCancellationWithoutUpstream(t *testing.T) {
	client, calls := multipartSignerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	signed, err := client.PresignMultipartPart(ctx, "fixture-bucket", "file", "upload", 1, multipartSHA256("part"), time.Minute)
	if signed != nil || !errors.Is(err, context.Canceled) || *calls != 0 {
		t.Fatal("cancelled signature created")
	}
}

func TestPresignMultipartPartRejectsUnsignedCredentials(t *testing.T) {
	for _, creds := range []*credentials.Credentials{
		credentials.NewStaticV4("", "", ""),
		credentials.NewStaticV2("fixture-access", "fixture-secret", ""),
	} {
		endpoint, _ := url.Parse("https://browser-s3.example.test")
		client, err := NewS3(Config{Endpoint: endpoint, Region: "us-east-1", Credentials: creds}, roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("signing contacted S3")
			return nil, errors.New("unexpected request")
		}))
		if err != nil {
			t.Fatal(err)
		}
		signed, err := NewS3Client(client).PresignMultipartPart(context.Background(), "fixture-bucket", "file", "upload", 1, multipartSHA256("part"), time.Minute)
		if signed != nil || err == nil {
			t.Fatal("unsigned or weaker URL escaped the adapter")
		}
	}
}

func TestPresignMultipartPartNativeIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("requires the disposable docker-compose.test.yml MinIO fixture on 127.0.0.1:19000")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	endpoint, _ := url.Parse("http://127.0.0.1:19000")
	client, err := NewS3(Config{Endpoint: endpoint, Region: "us-east-1", Credentials: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", "")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	bucket := "signed-part-fixture-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	key := "папка/part + # %.bin"
	if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal("cannot create isolated fixture bucket", apperror.Public(err).Code)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := client.RemoveBucket(cleanup, bucket); err != nil {
			t.Error("fixture bucket cleanup failed", apperror.Public(err).Code)
		}
	})
	core := NewS3Client(client)
	upload, err := core.NewMultipartUpload(ctx, bucket, key, minio.PutObjectOptions{})
	if err != nil {
		t.Fatal("cannot create fixture multipart", apperror.Public(err).Code)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := core.AbortMultipartUpload(cleanup, bucket, key, upload); err != nil {
			t.Error("fixture multipart cleanup failed", apperror.Public(err).Code)
		}
	})
	body := []byte("isolated signed multipart body")
	checksum := multipartSHA256(string(body))
	signed, err := core.PresignMultipartPart(ctx, bucket, key, upload, 1, checksum, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	put := func(method string, target *url.URL, checksum string, body []byte) int {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(body))
		if err != nil {
			t.Fatal("cannot create fixture request")
		}
		req.Header.Set("x-amz-checksum-sha256", checksum)
		response, err := httpClient.Do(req)
		if err != nil {
			// Do not include transport error text: it may contain the signed URL.
			t.Fatal("direct fixture request did not complete")
		}
		response.Body.Close()
		return response.StatusCode
	}
	for _, field := range []string{"key", "uploadId", "partNumber", "checksum", "method"} {
		t.Run("tamper "+field, func(t *testing.T) {
			target := *signed
			query := target.Query()
			header, method := checksum, http.MethodPut
			switch field {
			case "key":
				target.Path = "/" + bucket + "/other.bin"
				target.RawPath = ""
			case "uploadId":
				query.Set("uploadId", upload+"other")
			case "partNumber":
				query.Set("partNumber", "2")
			case "checksum":
				header = multipartSHA256("different checksum")
			case "method":
				method = http.MethodGet
			}
			target.RawQuery = query.Encode()
			if status := put(method, &target, header, body); status != http.StatusForbidden {
				t.Fatalf("tampered %s accepted or rejected unexpectedly: HTTP %d", field, status)
			}
		})
	}
	if status := put(http.MethodPut, signed, checksum, []byte("wrong bytes")); status != http.StatusBadRequest {
		t.Fatalf("signed checksum did not reject changed body: HTTP %d", status)
	}
	for attempt := range 2 {
		if status := put(http.MethodPut, signed, checksum, body); status != http.StatusOK {
			t.Fatalf("valid/replayed signed part %d failed: HTTP %d", attempt, status)
		}
	}
	page, err := core.ListObjectParts(ctx, bucket, key, upload, 0, 10)
	if err != nil || len(page.ObjectParts) != 1 || page.ObjectParts[0].PartNumber != 1 || page.ObjectParts[0].Size != int64(len(body)) {
		t.Fatal("direct part not confirmed by native ListParts")
	}
	if page.ObjectParts[0].ChecksumSHA256 != checksum {
		t.Fatal("native ListParts did not retain the signed part SHA256")
	}
	expired, err := core.PresignMultipartPart(ctx, bucket, key, upload, 2, checksum, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2100 * time.Millisecond)
	if status := put(http.MethodPut, expired, checksum, body); status != http.StatusForbidden {
		t.Fatalf("expired signature accepted: HTTP %d", status)
	}
}

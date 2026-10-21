package integration

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	objectwrite "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/write"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads"
)

func TestUploadIntegrityNativeIntegration(t *testing.T) {
	client, s3, bucket := fileFixture(t)
	ctx := context.Background()
	request := func(key, revision string, data []byte, value, mode string, want int) uploads.UploadReceipt {
		t.Helper()
		q := url.Values{"bucket": {bucket}, "key": {key}}
		req, err := http.NewRequest(http.MethodPut, client.Origin+client.Base+"/objects/upload?"+q.Encode(), bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", client.Origin)
		req.Header.Set("X-CSRF-Token", client.CSRF)
		req.Header.Set("X-Upload-Size", fmtSize(len(data)))
		req.Header.Set("X-Object-Revision", revision)
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("X-Upload-Checksum-Algorithm", "SHA256")
		req.Header.Set("X-Upload-Checksum-Value", value)
		req.Header.Set("X-Upload-Checksum-Type", mode)
		res, err := client.Http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != want {
			t.Fatalf("checksum upload got %d want %d: %s; %v", res.StatusCode, want, raw, err)
		}
		assertResponseContract(t, http.MethodPut, req.URL.Path, res.StatusCode, res.Header.Get("Content-Type"), raw)
		var receipt uploads.UploadReceipt
		if want == 200 && json.Unmarshal(raw, &receipt) != nil {
			t.Fatal("invalid receipt", string(raw))
		}
		return receipt
	}
	checksum := func(data []byte) string { h := sha256.Sum256(data); return base64.StdEncoding.EncodeToString(h[:]) }
	for _, scenario := range []string{"single", "empty", "multipart"} {
		t.Run(scenario, func(t *testing.T) {
			payload := []byte("verified checksum body")
			if scenario == "empty" {
				payload = nil
			}
			if scenario == "multipart" {
				payload = bytes.Repeat([]byte("m"), 17<<20)
			}
			value := checksum(payload)
			receipt := request(scenario, "absent", payload, value, "FULL_OBJECT", 200)
			if !receipt.OK || receipt.Size != int64(len(payload)) || receipt.Checksum == nil || receipt.Checksum.Expected.Value != value || receipt.Checksum.Computed.Value != value {
				t.Fatal("unverified receipt", receipt)
			}
			if receipt.Checksum.Verification != "api_verified" && receipt.Checksum.Verification != "storage_confirmed" {
				t.Fatal("incorrect integrity status", receipt.Checksum)
			}
			object, err := s3.GetObject(ctx, bucket, scenario, minio.GetObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			h := sha256.New()
			n, err := io.Copy(h, object)
			_ = object.Close()
			if err != nil || n != int64(len(payload)) || base64.StdEncoding.EncodeToString(h.Sum(nil)) != value {
				t.Fatal("stored bytes differ", n, err)
			}
		})
	}
	var state objectwrite.FileState
	q := url.Values{"bucket": {bucket}, "key": {"single"}}
	if json.Unmarshal(client.call("GET", "objects/prepare?"+q.Encode(), nil, 200), &state) != nil {
		t.Fatal("invalid destination state")
	}
	before, err := s3.StatObject(ctx, bucket, "single", minio.StatObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	request("single", state.Revision, []byte("replacement"), checksum([]byte("incorrect expectation")), "FULL_OBJECT", 400)
	request("single", state.Revision, []byte("replacement"), "malformed", "FULL_OBJECT", 400)
	request("single", state.Revision, []byte("replacement"), checksum([]byte("replacement"))+"-1", "COMPOSITE", 400)
	request("single", "absent", []byte("replacement"), checksum([]byte("replacement")), "FULL_OBJECT", 409)
	after, err := s3.StatObject(ctx, bucket, "single", minio.StatObjectOptions{})
	if err != nil || before.ETag != after.ETag || before.Size != after.Size {
		t.Fatal("rejected upload changed destination", before, after, err)
	}
	request("missing", "absent", []byte("body"), checksum([]byte("wrong")), "FULL_OBJECT", 400)
	if _, err = s3.StatObject(ctx, bucket, "missing", minio.StatObjectOptions{}); minio.ToErrorResponse(err).Code != "NoSuchKey" {
		t.Fatal("mismatch created object", err)
	}
	// Verify the isolated server actually rejects a wrong native Content-MD5,
	// independently of the API's pre-stage check and without touching user data.
	bad := md5.Sum([]byte("different"))
	_, err = (minio.Core{Client: s3}).PutObject(ctx, bucket, "single", strings.NewReader("replacement"), 11, base64.StdEncoding.EncodeToString(bad[:]), "", minio.PutObjectOptions{})
	if minio.ToErrorResponse(err).Code != "BadDigest" {
		t.Fatal("fixture did not reject native checksum", err)
	}
	after, err = s3.StatObject(ctx, bucket, "single", minio.StatObjectOptions{})
	if err != nil || before.ETag != after.ETag {
		t.Fatal("native checksum rejection changed destination", after, err)
	}
}

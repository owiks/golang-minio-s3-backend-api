package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	objectwrite "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/write"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads/integrity"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads/multipart"
)

func managedFixture(t *testing.T) (*apiClient, *minio.Client, string) {
	return fileFixture(t, func(cfg *config.Config) {
		cfg.ConditionalCompleteEndpoints = map[string]bool{"http://127.0.0.1:19000": true}
	})
}

func managedCreate(t *testing.T, c *apiClient, bucket, key, revision string, size int64) multipart.Snapshot {
	t.Helper()
	var v multipart.Snapshot
	if err := json.Unmarshal(c.call("POST", "objects/multipart", multipart.CreateRequest{Bucket: bucket, Key: key, Revision: revision, Size: size, ContentType: "application/octet-stream"}, 201), &v); err != nil {
		t.Fatal(err)
	}
	if v.State != "active" || v.ID == "" || v.PartCount < 1 {
		t.Fatal(v)
	}
	return v
}
func partDigest(data []byte) integrity.Spec {
	sum := sha256.Sum256(data)
	return integrity.Spec{Algorithm: "SHA256", Encoding: "base64", Value: base64.StdEncoding.EncodeToString(sum[:]), Type: "FULL_OBJECT", Scope: "PART"}
}
func managedPart(t *testing.T, c *apiClient, id string, n int, data []byte, want int) multipart.Snapshot {
	t.Helper()
	request, err := http.NewRequest("PUT", c.Origin+c.Base+"/objects/multipart/"+id+"/parts/"+fmtSize(n), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", c.Origin)
	request.Header.Set("X-CSRF-Token", c.CSRF)
	request.Header.Set("X-Upload-Size", fmtSize(len(data)))
	request.Header.Set("X-Part-Checksum-SHA256", partDigest(data).Value)
	response, err := c.Http.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != want {
		t.Fatalf("part: %d want%d: %s", response.StatusCode, want, body)
	}
	var v multipart.Snapshot
	if want == 200 {
		if err = json.Unmarshal(body, &v); err != nil {
			t.Fatal(err)
		}
	}
	return v
}
func managedState(t *testing.T, c *apiClient, id string) multipart.Snapshot {
	t.Helper()
	var v multipart.Snapshot
	if err := json.Unmarshal(c.call("GET", "objects/multipart/"+id, nil, 200), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func managedComplete(t *testing.T, c *apiClient, id string) multipart.Snapshot {
	t.Helper()
	var v multipart.Snapshot
	if err := json.Unmarshal(c.call("POST", "objects/multipart/"+id+"/complete", nil, 200), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestManagedMultipartResumeNativeIntegration(t *testing.T) {
	c, s3, bucket := managedFixture(t)
	ctx := context.Background()
	key := "managed/Привет + # %.bin"
	data := bytes.Repeat([]byte("x"), 17<<20)
	v := managedCreate(t, c, bucket, key, "absent", int64(len(data)))
	if v.PartCount != 2 || v.PartSize != 16<<20 {
		t.Fatal(v)
	}
	first := managedPart(t, c, v.ID, 1, data[:16<<20], 200)
	if len(first.Parts) != 1 || first.Parts[0].Number != 1 {
		t.Fatal(first)
	}
	// A new status request is the resume boundary; completed parts are sourced
	// from ListParts, so only the absent second part must be transmitted.
	resumed := managedState(t, c, v.ID)
	if len(resumed.Parts) != 1 {
		t.Fatal(resumed)
	}
	managedPart(t, c, v.ID, 1, data[:16<<20], 200)
	managedPart(t, c, v.ID, 2, data[16<<20:], 200)
	other := loginTest(t, c.Origin, "minio-web-test", "minio-web-test-secret")
	other.call("GET", "objects/multipart/"+v.ID, nil, 404)
	other.call("POST", "objects/multipart/"+v.ID+"/complete", nil, 404)
	other.call("DELETE", "objects/multipart/"+v.ID, nil, 404)
	final := managedComplete(t, c, v.ID)
	if final.State != "completed" || final.Receipt == nil || !final.Receipt.OK || final.Receipt.Size != int64(len(data)) {
		t.Fatal(final)
	}
	again := managedComplete(t, c, v.ID)
	if again.Receipt.ETag != final.Receipt.ETag || again.Receipt.VersionID != final.Receipt.VersionID {
		t.Fatal("repeat completion changed receipt")
	}
	object, err := s3.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(object)
	_ = object.Close()
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("content changed", len(actual), err)
	}
	// Empty files use the same controlled protocol and checksum contract.
	empty := managedCreate(t, c, bucket, "managed/empty.bin", "absent", 0)
	managedPart(t, c, empty.ID, 1, nil, 200)
	empty = managedComplete(t, c, empty.ID)
	if empty.Receipt == nil || empty.Receipt.Size != 0 || !empty.Receipt.OK {
		t.Fatal(empty)
	}
}

func TestManagedMultipartDestinationConflictNativeIntegration(t *testing.T) {
	c, s3, bucket := managedFixture(t)
	ctx := context.Background()
	key := "managed/conflict.bin"
	data := []byte("managed")
	v := managedCreate(t, c, bucket, key, "absent", int64(len(data)))
	managedPart(t, c, v.ID, 1, data, 200)
	if _, err := s3.PutObject(ctx, bucket, key, strings.NewReader("external"), 8, minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	result := c.call("POST", "objects/multipart/"+v.ID+"/complete", nil, 409)
	if !bytes.Contains(result, []byte("DestinationChanged")) {
		t.Fatal(string(result))
	}
	info, err := s3.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil || info.Size != 8 {
		t.Fatal(info, err)
	}
	c.call("DELETE", "objects/multipart/"+v.ID, nil, 200)
	query := url.Values{"bucket": {bucket}, "key": {key}}
	var state objectwrite.FileState
	if err = json.Unmarshal(c.call("GET", "objects/prepare?"+query.Encode(), nil, 200), &state); err != nil {
		t.Fatal(err)
	}
	v = managedCreate(t, c, bucket, key, state.Revision, int64(len(data)))
	managedPart(t, c, v.ID, 1, data, 200)
	v = managedComplete(t, c, v.ID)
	if v.Receipt == nil || !v.Receipt.OK {
		t.Fatal(v)
	}
}

func TestManagedDirectPartNativeIntegration(t *testing.T) {
	c, s3, bucket := managedFixture(t)
	data := []byte("direct S3 part")
	v := managedCreate(t, c, bucket, "direct/file.bin", "absent", int64(len(data)))
	var signed multipart.SignedPart
	if err := json.Unmarshal(c.call("POST", "objects/multipart/"+v.ID+"/parts/1/sign", multipart.PartRequest{Checksum: partDigest(data)}, 200), &signed); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(signed.Method, signed.URL, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for k, value := range signed.Headers {
		request.Header.Set(k, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("directpart%d: %s", response.StatusCode, body)
	}
	state := managedState(t, c, v.ID)
	if len(state.Parts) != 1 || state.Parts[0].Size != int64(len(data)) {
		t.Fatal(state)
	}
	final := managedComplete(t, c, v.ID)
	if final.Receipt == nil || !final.Receipt.OK {
		t.Fatal(final)
	}
	object, err := s3.GetObject(context.Background(), bucket, "direct/file.bin", minio.GetObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(object)
	_ = object.Close()
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("direct content", err)
	}
	// A matching digest for an oversized body can pass S3 PutPart. The API does
	// not claim to prevent those transferred bytes: ListParts blocks the commit.
	oversized := []byte("oversized")
	v = managedCreate(t, c, bucket, "direct/oversized.bin", "absent", 1)
	if err = json.Unmarshal(c.call("POST", "objects/multipart/"+v.ID+"/parts/1/sign", multipart.PartRequest{Checksum: partDigest(oversized)}, 200), &signed); err != nil {
		t.Fatal(err)
	}
	request, _ = http.NewRequest(signed.Method, signed.URL, bytes.NewReader(oversized))
	for k, value := range signed.Headers {
		request.Header.Set(k, value)
	}
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	c.call("POST", "objects/multipart/"+v.ID+"/complete", nil, 409)
	if _, err = s3.StatObject(context.Background(), bucket, "direct/oversized.bin", minio.StatObjectOptions{}); minio.ToErrorResponse(err).Code != "NoSuchKey" {
		t.Fatal("oversized was committed", err)
	}
	c.call("DELETE", "objects/multipart/"+v.ID, nil, 200)
}

func TestManagedMultipartLogoutCleanupNativeIntegration(t *testing.T) {
	c, s3, bucket := managedFixture(t)
	v := managedCreate(t, c, bucket, "logout/file.bin", "absent", 1)
	managedPart(t, c, v.ID, 1, []byte("x"), 200)
	status, data := c.request("DELETE", c.Base, nil)
	if status != 200 {
		t.Fatal(status, string(data))
	}
	for item := range s3.ListIncompleteUploads(context.Background(), bucket, "logout/", true) {
		if item.Err != nil {
			t.Fatal(item.Err)
		}
		t.Fatal("logout left managed upload", item.Key)
	}
}

func TestManagedCompositeChecksumNativeIntegration(t *testing.T) {
	c, s3, bucket := managedFixture(t)
	data := []byte("expected composite body")
	partSum := sha256.Sum256(data)
	sum := sha256.Sum256(partSum[:])
	expected := integrity.Spec{Algorithm: "SHA256", Encoding: "base64", Value: base64.StdEncoding.EncodeToString(sum[:]) + "-1", Type: "COMPOSITE", Scope: "OBJECT"}
	for _, wrong := range []bool{false, true} {
		key := "composite/good.bin"
		spec := expected
		if wrong {
			key = "composite/wrong.bin"
			v := sha256.Sum256([]byte("wrong"))
			spec.Value = base64.StdEncoding.EncodeToString(v[:]) + "-1"
		}
		var operation multipart.Snapshot
		if err := json.Unmarshal(c.call("POST", "objects/multipart", multipart.CreateRequest{Bucket: bucket, Key: key, Revision: "absent", Size: int64(len(data)), Checksum: &spec}, 201), &operation); err != nil {
			t.Fatal(err)
		}
		managedPart(t, c, operation.ID, 1, data, 200)
		if wrong {
			response := c.call("POST", "objects/multipart/"+operation.ID+"/complete", nil, 400)
			if !bytes.Contains(response, []byte("ChecksumMismatch")) {
				t.Fatal(string(response))
			}
			if _, err := s3.StatObject(context.Background(), bucket, key, minio.StatObjectOptions{}); minio.ToErrorResponse(err).Code != "NoSuchKey" {
				t.Fatal("mismatched object committed", err)
			}
			c.call("DELETE", "objects/multipart/"+operation.ID, nil, 200)
			continue
		}
		operation = managedComplete(t, c, operation.ID)
		if operation.Receipt == nil || !operation.Receipt.OK || operation.Receipt.Checksum == nil || operation.Receipt.Checksum.Expected.Value != expected.Value {
			t.Fatal(operation)
		}
	}
}

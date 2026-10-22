package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	apihttp "github.com/owiks/golang-minio-s3-backend-api/src/http/api"
	operationcontrollers "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/operations/controllers"
	objectwrite "github.com/owiks/golang-minio-s3-backend-api/src/services/objects/write"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
)

func nativeOperationsFixture(t *testing.T) (*apiClient, *minio.Client, string) {
	t.Helper()
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("requires isolated loopback MinIO fixture")
	}
	cfg := testConfig()
	cfg.Endpoints.Allowed["http://127.0.0.1:19000"] = true
	app := runtimesvc.New(cfg)
	srv := httptest.NewServer(apihttp.NewHandler(app))
	t.Cleanup(func() {
		srv.Close()
		app.Close()
	})
	native, err := minio.New("127.0.0.1:19000", &minio.Options{Creds: credentials.NewStaticV4("minio-web-test", "minio-web-test-secret", ""), Region: "us-east-1", BucketLookup: minio.BucketLookupPath})
	if err != nil {
		t.Fatal(err)
	}
	bucket := "operations-" + strings.ToLower(strings.NewReplacer("_", "a", "-", "b").Replace(securetoken.New()[:10]))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := native.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for object := range native.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true, WithVersions: true}) {
			if object.Err != nil {
				t.Error("list fixture cleanup", object.Err)
				break
			}
			if err := native.RemoveObject(ctx, bucket, object.Key, minio.RemoveObjectOptions{VersionID: object.VersionID}); err != nil {
				t.Error("remove fixture object", err)
			}
		}
		if err := native.RemoveBucket(ctx, bucket); err != nil {
			t.Error("remove fixture bucket", err)
		}
	})
	workers, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		app.RunOperations(workers)
	}()
	// Cleanup is LIFO: stop/join workers before deleting their input and closing
	// the HTTP server or auth/storage repositories.
	t.Cleanup(func() {
		stop()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("operation workers did not stop")
		}
	})
	return loginTest(t, srv.URL, "minio-web-test", "minio-web-test-secret"), native, bucket
}

func nativeOperationRequest(t *testing.T, c *apiClient, ctx context.Context, method, path string, body any, key string, want int) (http.Header, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Origin+c.Base+"/"+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", c.Origin)
	req.Header.Set("X-CSRF-Token", c.CSRF)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res, err := c.Http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 2<<20+1))
	if err != nil || len(raw) > 2<<20 {
		t.Fatal("operation response exceeded limit or failed to read", err)
	}
	if res.StatusCode != want {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, res.StatusCode, want, raw)
	}
	assertResponseContract(t, method, req.URL.Path, res.StatusCode, res.Header.Get("Content-Type"), raw)
	return res.Header.Clone(), raw
}

func nativeOperationView(t *testing.T, raw []byte) operationcontrollers.View {
	t.Helper()
	var result operationcontrollers.View
	if err := json.Unmarshal(raw, &result); err != nil || result.ID == "" {
		t.Fatal("invalid operation response", string(raw), err)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	private := map[string]bool{"scope": true, "spec": true, "actorID": true, "connectionID": true, "workerID": true, "leaseID": true, "leaseUntil": true, "deadline": true, "inFlight": true, "lastServedAt": true, "token": true, "seen": true, "sourceRevision": true, "destinationRevision": true}
	var check func(any)
	check = func(value any) {
		switch current := value.(type) {
		case map[string]any:
			for key, nested := range current {
				if private[key] {
					t.Fatal("operation exposed internal state", key)
				}
				check(nested)
			}
		case []any:
			for _, nested := range current {
				check(nested)
			}
		}
	}
	check(decoded)
	if bytes.Contains(raw, []byte("minio-web-test-secret")) {
		t.Fatal("operation exposed fixture credential")
	}
	return result
}

func awaitNativeOperation(t *testing.T, c *apiClient, ctx context.Context, id string) operationcontrollers.View {
	t.Helper()
	for {
		_, raw := nativeOperationRequest(t, c, ctx, "GET", "operations/"+url.PathEscape(id), nil, "", 200)
		result := nativeOperationView(t, raw)
		switch result.State {
		case "completed":
			return result
		case "failed", "unknown", "cancelled":
			t.Fatalf("native operation stopped: %s", raw)
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			t.Fatal("native operation exceeded deadline", ctx.Err())
		case <-timer.C:
		}
	}
}

func TestDurableTransferOperationNativeIntegration(t *testing.T) {
	c, native, bucket := nativeOperationsFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	data := map[string]string{"source/copy.txt": "copy", "source/move.txt": "move me"}
	items := []transfers.FileTransferRequest{}
	for _, source := range []string{"source/copy.txt", "source/move.txt"} {
		body := data[source]
		if _, err := native.PutObject(ctx, bucket, source, strings.NewReader(body), int64(len(body)), minio.PutObjectOptions{ContentType: "text/plain"}); err != nil {
			t.Fatal(err)
		}
		var state objectwrite.FileState
		if err := json.Unmarshal(c.call("GET", "objects/prepare?"+url.Values{"bucket": {bucket}, "key": {source}}.Encode(), nil, 200), &state); err != nil || !state.Exists || state.Revision == "" {
			t.Fatal("source revision", state, err)
		}
		items = append(items, transfers.FileTransferRequest{SourceBucket: bucket, SourceKey: source, SourceRevision: state.Revision, DestinationBucket: bucket, DestinationKey: "result/" + strings.TrimPrefix(source, "source/"), DestinationRevision: "absent", Move: strings.HasSuffix(source, "move.txt")})
	}
	body := operationcontrollers.TransfersRequest{Items: items}
	headers, raw := nativeOperationRequest(t, c, ctx, "POST", "operations/transfers", body, "native-two-files", 202)
	created := nativeOperationView(t, raw)
	if headers.Get("Location") != c.Base+"/operations/"+created.ID || headers.Get("X-Command-ID") == "" || headers.Get("Idempotency-Replayed") != "" {
		t.Fatal("create operation envelope", headers)
	}
	replayHeaders, raw := nativeOperationRequest(t, c, ctx, "POST", "operations/transfers", body, "native-two-files", 202)
	if replay := nativeOperationView(t, raw); replay.ID != created.ID || replayHeaders.Get("X-Command-ID") != headers.Get("X-Command-ID") || replayHeaders.Get("Idempotency-Replayed") != "true" {
		t.Fatal("duplicate submission created another job", replayHeaders, replay)
	}
	changed := operationcontrollers.TransfersRequest{Items: append([]transfers.FileTransferRequest(nil), items...)}
	changed.Items[0].DestinationKey = "result/different.txt"
	nativeOperationRequest(t, c, ctx, "POST", "operations/transfers", changed, "native-two-files", 409)
	result := awaitNativeOperation(t, c, ctx, created.ID)
	if result.Kind != "transfer" || result.Total != 2 || result.Completed != 2 || result.Errors != 0 || result.Retryable || len(result.Items) != 2 {
		t.Fatal("transfer operation results", result)
	}
	for index, item := range items {
		if got := result.Items[index]; got.Index != index || got.State != "success" || !got.Copied || got.Moved != item.Move || got.Code != "" {
			t.Fatal("incorrect per-file result", got)
		}
		object, err := native.GetObject(ctx, bucket, item.DestinationKey, minio.GetObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		content, readErr := io.ReadAll(object)
		closeErr := object.Close()
		if readErr != nil || closeErr != nil || string(content) != data[item.SourceKey] {
			t.Fatal("copied content", string(content), readErr, closeErr)
		}
		_, statErr := native.StatObject(ctx, bucket, item.SourceKey, minio.StatObjectOptions{})
		if item.Move && minio.ToErrorResponse(statErr).Code != "NoSuchKey" || !item.Move && statErr != nil {
			t.Fatal("move or copy changed source incorrectly", item.SourceKey, statErr)
		}
	}
	_, raw = nativeOperationRequest(t, c, ctx, "GET", "operations", nil, "", 200)
	var list []operationcontrollers.View
	if err := json.Unmarshal(raw, &list); err != nil || len(list) != 1 || list[0].ID != created.ID {
		t.Fatal("operation list", string(raw), err)
	}
	other := loginTest(t, c.Origin, "minio-web-test", "minio-web-test-secret")
	nativeOperationRequest(t, other, ctx, "GET", "operations/"+created.ID, nil, "", 404)
	_, raw = nativeOperationRequest(t, other, ctx, "GET", "operations", nil, "", 200)
	if err := json.Unmarshal(raw, &list); err != nil || len(list) != 0 {
		t.Fatal("another API access saw actor operations", string(raw), err)
	}
}

func TestDurableScanOperationNativePaginationIntegration(t *testing.T) {
	c, native, bucket := nativeOperationsFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	const count = 501
	for index := range count {
		if _, err := native.PutObject(ctx, bucket, fmt.Sprintf("measured/%04d.txt", index), strings.NewReader("x"), 1, minio.PutObjectOptions{}); err != nil {
			t.Fatal("create scan fixture", index, err)
		}
	}
	if _, err := native.PutObject(ctx, bucket, "measured-other/excluded.txt", strings.NewReader("outside"), 7, minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	headers, raw := nativeOperationRequest(t, c, ctx, "POST", "operations/scans", operationcontrollers.ScanRequest{Bucket: bucket, Prefix: "measured/"}, "native-scan", 202)
	created := nativeOperationView(t, raw)
	if headers.Get("Location") != c.Base+"/operations/"+created.ID {
		t.Fatal("scan location", headers)
	}
	result := awaitNativeOperation(t, c, ctx, created.ID)
	if result.Kind != "scan" || result.Scan == nil || result.Scan.Objects != count || result.Scan.Bytes != count || result.Scan.Pages != 2 || len(result.Items) != 0 || result.Errors != 0 {
		t.Fatal("scan skipped a page, double counted or broadened prefix", result)
	}
}

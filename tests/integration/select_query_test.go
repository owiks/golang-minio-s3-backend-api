package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	selectcontroller "github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/selectquery/controllers"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/presentation"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/stream"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/selectquery"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
	"github.com/owiks/golang-minio-s3-backend-api/tests/testsession"
)

func readSelectEvents(t *testing.T, data []byte) ([]byte, *selectquery.Result, *presentation.Error) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var output bytes.Buffer
	var final *selectquery.Result
	var failure *presentation.Error
	for {
		var event stream.Event[selectquery.Result]
		err := decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal("invalid NDJSON", err)
		}
		if final != nil || failure != nil {
			t.Fatal("event after terminal")
		}
		if event.Data != nil {
			if event.Data.Encoding != "base64" {
				t.Fatal(event.Data)
			}
			chunk, err := base64.StdEncoding.DecodeString(event.Data.Bytes)
			if err != nil || len(chunk) > selectquery.ChunkBytes {
				t.Fatal("invalid chunk", err)
			}
			output.Write(chunk)
		}
		final, failure = event.Result, event.Error
	}
	if final == nil && failure == nil {
		t.Fatal("missing terminal event", string(data))
	}
	return output.Bytes(), final, failure
}

func TestSelectNativeCSVAndJSONIntegration(t *testing.T) {
	client, s3, bucket := fileFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name, body, expression, want string
		input                        selectquery.Input
	}{
		{"columns.csv", "id,name\n1,one\n2,two\n3,three\n", "SELECT s.name FROM S3Object s WHERE CAST(s.id AS INT) > 1", `"name":"two"`, selectquery.Input{CSV: &selectquery.CSVInput{Header: "USE"}}},
		{"aggregate.csv", "id,name\n1,one\n2,two\n3,three\n", "SELECT COUNT(*) FROM S3Object", `3`, selectquery.Input{CSV: &selectquery.CSVInput{Header: "USE"}}},
		{"delimiter.csv", "id;name\n1;one\n2;two\n", "SELECT s.name FROM S3Object s WHERE CAST(s.id AS INT) > 1", `"name":"two"`, selectquery.Input{CSV: &selectquery.CSVInput{Header: "USE", FieldDelimiter: ";"}}},
		{"lines.json", "{\"id\":1,\"name\":\"one\"}\n{\"id\":2,\"name\":\"two\"}\n", "SELECT s.name FROM S3Object s WHERE s.id > 1", `"name":"two"`, selectquery.Input{JSON: &selectquery.JSONInput{Type: "LINES"}}},
		{"document.json", "{\"id\":2,\"name\":\"two\"}", "SELECT s.name FROM S3Object s WHERE s.id > 1", `"name":"two"`, selectquery.Input{JSON: &selectquery.JSONInput{Type: "DOCUMENT"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s3.PutObject(ctx, bucket, tc.name, strings.NewReader(tc.body), int64(len(tc.body)), minio.PutObjectOptions{}); err != nil {
				t.Fatal(err)
			}
			request := selectquery.Request{Expression: tc.expression, Input: tc.input, Output: selectquery.Output{JSON: &selectquery.JSONOutput{RecordDelimiter: "\n"}}}
			path := "objects/select?" + url.Values{"bucket": {bucket}, "key": {tc.name}}.Encode()
			status, data := client.request("POST", client.Base+"/"+path, request)
			if status == 501 {
				t.Skip("isolated MinIO explicitly does not support Select: " + string(data))
			}
			if status != 200 {
				t.Fatalf("Select %d: %s", status, data)
			}
			output, final, failure := readSelectEvents(t, data)
			if failure != nil || final == nil || !final.Complete || !strings.Contains(string(output), tc.want) {
				t.Fatal("server query failed", string(output), final, failure)
			}
			if final.Statistics == nil || final.Statistics.BytesScanned <= 0 || final.Statistics.BytesReturned != int64(len(output)) {
				t.Fatal("missing real final stats", final)
			}
			t.Logf("Select %s confirmed; source=%d, returned=%d", tc.name, len(tc.body), len(output))
		})
	}
	invalid := selectquery.Request{Expression: "SELECT FROM", Input: selectquery.Input{CSV: &selectquery.CSVInput{Header: "USE"}}, Output: selectquery.Output{JSON: &selectquery.JSONOutput{}}}
	status, data := client.request("POST", client.Base+"/objects/select?"+url.Values{"bucket": {bucket}, "key": {"columns.csv"}}.Encode(), invalid)
	if status == 200 {
		_, final, failure := readSelectEvents(t, data)
		if final != nil || failure == nil {
			t.Fatal("bad SQL succeeded", string(data))
		}
	} else if status != 400 && status != 501 {
		t.Fatalf("bad SQL returned %d: %s", status, data)
	}
	client.call("POST", "objects/select?"+url.Values{"bucket": {bucket}, "key": {"columns.csv"}, "versionId": {"version"}}.Encode(), invalid, 400)
}

func TestSelectNativeLargeResultAndLimitIntegration(t *testing.T) {
	client, s3, bucket := fileFixture(t)
	body := strings.Repeat(strings.Repeat("x", 511)+"\n", 10240)
	if _, err := s3.PutObject(context.Background(), bucket, "large.csv", strings.NewReader(body), int64(len(body)), minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	request := selectquery.Request{Expression: "SELECT * FROM S3Object", Input: selectquery.Input{CSV: &selectquery.CSVInput{Header: "NONE"}}, Output: selectquery.Output{CSV: &selectquery.CSVOutput{RecordDelimiter: "\n"}}}
	path := "objects/select?" + url.Values{"bucket": {bucket}, "key": {"large.csv"}}.Encode()
	status, data := client.request("POST", client.Base+"/"+path, request)
	if status == 501 {
		t.Skip("isolated fixture explicitly unsupported")
	}
	if status != 200 {
		t.Fatalf("Select %d: %s", status, data)
	}
	output, final, failure := readSelectEvents(t, data)
	if final == nil || failure != nil || len(output) <= 4<<20 || string(output) != body {
		t.Fatal("large stream failed", len(output), final, failure)
	}
	request.MaxResultBytes = 64 << 10
	data = client.call("POST", path, request, 200)
	output, final, failure = readSelectEvents(t, data)
	if final != nil || failure == nil || failure.Code != "SelectResultLimit" || len(output) > 64<<10 {
		t.Fatal("result limit did not terminate partial stream", len(output), final, failure)
	}
}

func TestSelectNativeStreamDeliveryFailureIntegration(t *testing.T) {
	_, s3, bucket := fileFixture(t)
	body := strings.Repeat("one,two\n", 32768)
	if _, err := s3.PutObject(context.Background(), bucket, "delivery.csv", strings.NewReader(body), int64(len(body)), minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	app := runtime.New(testConfig())
	defer app.Close()
	for _, phase := range []string{"write", "flush"} {
		t.Run(phase, func(t *testing.T) {
			ctx, trace := telemetry.Start(context.Background(), "POST Select")
			request := httptest.NewRequest("POST", "/?"+url.Values{"bucket": {bucket}, "key": {"delivery.csv"}}.Encode(), strings.NewReader(`{"expression":"SELECT * FROM S3Object","input":{"csv":{}},"output":{"csv":{}}}`)).WithContext(ctx)
			request.Header.Set("Content-Type", "application/json")
			writer := &rejectingStreamWriter{ResponseRecorder: httptest.NewRecorder(), phase: phase}
			if err := selectcontroller.HandleSelectObject(app, writer, request, testsession.Native(t)); err != nil {
				t.Fatal("stream returned second HTTP error", err)
			}
			if writer.writes != 1 || strings.Contains(writer.Body.String(), `"result"`) || trace.ResultName(ctx, 200) != "cancelled" || trace.Code != "StreamWriteFailed" {
				t.Fatal("delivery failure lost", writer.writes, writer.Body.String(), trace.Result, trace.Code)
			}
		})
	}
}

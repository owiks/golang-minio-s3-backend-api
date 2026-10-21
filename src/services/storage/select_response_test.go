package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"minio-web/api/src/services/apperror"
	"minio-web/api/tests/testmessage"
)

func selectTestFrame(kind string, payload []byte) []byte {
	var h bytes.Buffer
	for _, v := range [][2]string{{":message-type", "event"}, {":event-type", kind}} {
		h.WriteByte(byte(len(v[0])))
		h.WriteString(v[0])
		h.WriteByte(7)
		_ = binary.Write(&h, binary.BigEndian, uint16(len(v[1])))
		h.WriteString(v[1])
	}
	if kind == "Stats" || kind == "Progress" {
		name, value := ":content-type", "text/xml"
		h.WriteByte(byte(len(name)))
		h.WriteString(name)
		h.WriteByte(7)
		_ = binary.Write(&h, binary.BigEndian, uint16(len(value)))
		h.WriteString(value)
	}
	frame := make([]byte, 12, 16+h.Len()+len(payload))
	binary.BigEndian.PutUint32(frame, uint32(16+h.Len()+len(payload)))
	binary.BigEndian.PutUint32(frame[4:], uint32(h.Len()))
	binary.BigEndian.PutUint32(frame[8:], crc32.ChecksumIEEE(frame[:8]))
	frame = append(frame, h.Bytes()...)
	frame = append(frame, payload...)
	return binary.BigEndian.AppendUint32(frame, crc32.ChecksumIEEE(frame))
}

func selectTestErrorFrame(code string) []byte {
	var h bytes.Buffer
	for _, v := range [][2]string{{":message-type", "error"}, {":error-code", code}, {":error-message", "secret SQL and object content must not leak"}} {
		h.WriteByte(byte(len(v[0])))
		h.WriteString(v[0])
		h.WriteByte(7)
		_ = binary.Write(&h, binary.BigEndian, uint16(len(v[1])))
		h.WriteString(v[1])
	}
	frame := make([]byte, 12, 16+h.Len())
	binary.BigEndian.PutUint32(frame, uint32(16+h.Len()))
	binary.BigEndian.PutUint32(frame[4:], uint32(h.Len()))
	binary.BigEndian.PutUint32(frame[8:], crc32.ChecksumIEEE(frame[:8]))
	frame = append(frame, h.Bytes()...)
	return binary.BigEndian.AppendUint32(frame, crc32.ChecksumIEEE(frame))
}

func selectTestConfig(t *testing.T, server *httptest.Server) Config {
	t.Helper()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return Config{Endpoint: u, Credentials: credentials.NewStaticV4("fixture", "fixture-secret", ""), Region: "us-east-1"}
}
func selectTestOptions() minio.SelectObjectOptions {
	return minio.SelectObjectOptions{Expression: "SELECT * FROM S3Object", ExpressionType: minio.QueryExpressionTypeSQL, InputSerialization: minio.SelectObjectInputSerialization{CSV: &minio.CSVInputOptions{}}, OutputSerialization: minio.SelectObjectOutputSerialization{CSV: &minio.CSVOutputOptions{}}}
}

func TestSelectClassificationIsExact(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		status       int
		want         bool
	}{{"POST", "/bucket/key?select=&select-type=2", 200, true}, {"POST", "/bucket/key?select=&select-type=2&versionId=x", 200, false}, {"POST", "/bucket/key?select=&select=&select-type=2", 200, false}, {"POST", "/bucket/key?select=x&select-type=2", 200, false}, {"POST", "/bucket/key?select-type=2", 200, false}, {"POST", "/bucket?select=&select-type=2", 200, false}, {"POST", "/bucket/key?select=&select-type=1", 200, false}, {"GET", "/bucket/key?select=&select-type=2", 200, false}, {"POST", "/bucket/key?select=&select-type=2", 403, false}, {"POST", "/bucket/key?uploadId=x", 200, false}} {
		t.Run(tc.method+tc.path+fmt.Sprint(tc.status), func(t *testing.T) {
			u, _ := url.Parse("http://fixture.invalid" + tc.path)
			response := &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(""))}
			response, matched, err := selectResponse(context.Background(), tc.method, u, response)
			if response != nil {
				defer response.Body.Close()
			}
			if err != nil || matched != tc.want {
				t.Fatal(matched, err)
			}
		})
	}
}

func TestSelectSDKStreamsBeyondMetadataLimitAndReadsFinalStats(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Query().Get("select-type") != "2" || !r.URL.Query().Has("select") {
			t.Error("wrong Select operation", r.Method, r.URL)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		chunk := bytes.Repeat([]byte("x"), 128<<10)
		for range 40 {
			_, _ = w.Write(selectTestFrame("Records", chunk))
		}
		_, _ = w.Write(selectTestFrame("Stats", []byte(`<Stats><BytesScanned>6000000</BytesScanned><BytesProcessed>6000000</BytesProcessed><BytesReturned>5242880</BytesReturned></Stats>`)))
		_, _ = w.Write(selectTestFrame("End", nil))
	}))
	defer server.Close()
	reader, err := OpenSelect(context.Background(), selectTestConfig(t, server), server.Client().Transport, "bucket", "key", selectTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if reader.FinalStats() != nil {
		t.Fatal("statistics exposed before EOF")
	}
	n, err := io.Copy(io.Discard, reader)
	if err != nil || n != 5<<20 || calls.Load() != 1 {
		t.Fatal(n, err, calls.Load())
	}
	stats := reader.FinalStats()
	if stats == nil || stats.BytesReturned != 5<<20 || stats.BytesProcessed != 6000000 {
		t.Fatal(stats)
	}
}

func TestSelectSDKRejectsMalformedEventsWithoutLargeAllocations(t *testing.T) {
	valid := selectTestFrame("Records", []byte("row\n"))
	badCRC := append([]byte(nil), valid...)
	badCRC[len(badCRC)-1] ^= 1
	huge := make([]byte, 12)
	binary.BigEndian.PutUint32(huge, 1<<31)
	binary.BigEndian.PutUint32(huge[8:], crc32.ChecksumIEEE(huge[:8]))
	badEnd := selectTestFrame("End", nil)
	badEnd[len(badEnd)-1] ^= 1
	for name, data := range map[string][]byte{"crc": badCRC, "too large": huge, "short": valid[:len(valid)-1], "missing end": valid, "bad end": badEnd, "stats shape": selectTestFrame("Stats", []byte(`<Stats/>`)), "unknown event": selectTestFrame("Anything", nil)} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = w.Write(data) }))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			reader, err := OpenSelect(ctx, selectTestConfig(t, server), server.Client().Transport, "bucket", "key", selectTestOptions())
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			_, err = io.Copy(io.Discard, reader)
			if err == nil || calls.Load() != 1 || reader.FinalStats() != nil {
				t.Fatal("invalid stream accepted", err, calls.Load())
			}
		})
	}
}

func TestSelectCloseCancelsBlockedSDKAndUpstream(t *testing.T) {
	cancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(selectTestFrame("Records", []byte("row\n")))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}))
	defer server.Close()
	reader, err := OpenSelect(context.Background(), selectTestConfig(t, server), server.Client().Transport, "bucket", "key", selectTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 4)
	if _, err := io.ReadFull(reader, buffer); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := reader.Read(buffer); done <- err }()
	_ = reader.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled read succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("SDK reader leaked")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("upstream not cancelled")
	}
}

func TestSelectPreservesFiniteErrorAndUnknownPOSTLimits(t *testing.T) {
	for _, tc := range []struct {
		status int
		query  string
		size   int
	}{{403, "select=&select-type=2", maxS3ErrorResponse + 1}, {200, "select=&select-type=2&other=x", maxS3MetadataResponse + 1}} {
		t.Run(fmt.Sprint(tc.status)+tc.query, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write(bytes.Repeat([]byte("x"), tc.size))
			}))
			defer server.Close()
			client, err := NewS3(selectTestConfig(t, server), server.Client().Transport)
			if err != nil {
				t.Fatal(err)
			}
			if tc.status == 403 {
				_, err = client.SelectObjectContent(context.Background(), "bucket", "key", selectTestOptions())
			} else {
				u, _ := url.Parse(server.URL + "/bucket/key?" + tc.query)
				request, _ := http.NewRequest("POST", u.String(), nil)
				response, roundTripErr := (s3ResponseTransport{base: server.Client().Transport}).RoundTrip(request)
				if response != nil {
					_ = response.Body.Close()
				}
				err = roundTripErr
			}
			if err == nil || apperror.Public(err).Code != "S3ResponseTooLarge" {
				t.Fatal(err)
			}
		})
	}
}

func TestSelectDeadlineCancelsSilentUpstream(t *testing.T) {
	cancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	reader, err := OpenSelect(ctx, selectTestConfig(t, server), server.Client().Transport, "bucket", "key", selectTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	_, err = io.Copy(io.Discard, reader)
	if err == nil || apperror.Public(err).Code != "Timeout" {
		t.Fatal("deadline lost", err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("timed out upstream still active")
	}
}

func TestSelectFrameAndServiceBudgets(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame []byte
		count int
	}{
		{"event count", selectTestFrame("Cont", nil), maxSelectEvents + 1},
		{"upstream bytes", selectTestFrame("Records", bytes.Repeat([]byte("x"), maxSelectFrame-128)), 65},
	} {
		t.Run(tc.name, func(t *testing.T) {
			readers := make([]io.Reader, tc.count)
			for i := range readers {
				readers[i] = bytes.NewReader(tc.frame)
			}
			body := &selectBody{ctx: context.Background(), source: io.NopCloser(io.MultiReader(readers...))}
			_, err := io.Copy(io.Discard, body)
			if err == nil || apperror.Public(err).Code != "SelectUpstreamLimit" {
				t.Fatal("budget not enforced", err)
			}
		})
	}
	for _, name := range []string{"event-type", ":Event-Type", ":unknown"} {
		var header bytes.Buffer
		header.WriteByte(byte(len(name)))
		header.WriteString(name)
		header.WriteByte(7)
		_ = binary.Write(&header, binary.BigEndian, uint16(3))
		header.WriteString("End")
		if _, err := selectEventHeaders(header.Bytes()); err == nil {
			t.Fatal("header alias accepted", name)
		}
	}
}

func TestSelectSDKErrorsAfterRecordsArePartialAndSafe(t *testing.T) {
	for _, tc := range []struct{ native, code string }{{"AccessDenied", "AccessDenied"}, {"NotImplemented", "NotImplemented"}, {"InvalidExpression", "SelectQueryFailed"}, {"InternalError", "SelectStreamFailed"}} {
		t.Run(tc.native, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = w.Write(selectTestFrame("Records", []byte("partial row\n")))
				_, _ = w.Write(selectTestErrorFrame(tc.native))
			}))
			defer server.Close()
			reader, err := OpenSelect(context.Background(), selectTestConfig(t, server), server.Client().Transport, "bucket", "key", selectTestOptions())
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			data, err := io.ReadAll(reader)
			if string(data) != "partial row\n" || err == nil || apperror.Public(err).Code != tc.code || strings.Contains(testmessage.Russian(apperror.Public(err).Message), "secret") || reader.FinalStats() != nil || calls.Load() != 1 {
				t.Fatal(string(data), err, calls.Load())
			}
		})
	}
}

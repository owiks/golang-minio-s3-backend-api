package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/minio-go/v7/pkg/credentials"
	"minio-web/api/src/services/apperror"
)

type diagnosticTestBody struct {
	reader io.Reader
	bytes  atomic.Int64
	closes atomic.Int32
}

func (b *diagnosticTestBody) Read(p []byte) (int, error) {
	n, e := b.reader.Read(p)
	b.bytes.Add(int64(n))
	return n, e
}
func (b *diagnosticTestBody) Close() error { b.closes.Add(1); return nil }

type fragmentedDiagnostic struct{ reader io.Reader }

func (f fragmentedDiagnostic) Read(p []byte) (int, error) { return f.reader.Read(p[:min(len(p), 1)]) }

func TestDiagnosticRecordFramingAndLongSubscriptions(t *testing.T) {
	for _, fragmented := range []bool{false, true} {
		t.Run(fmt.Sprint(fragmented), func(t *testing.T) {
			const first = "{\n\"path\":\"quoted \\\" } [ \\\\ text\",\"nested\":[{},[1,2]]\n}"
			input := first + `{"node":"second"} null true 123 "value"` + "\n"
			var source io.Reader = strings.NewReader(input)
			if fragmented {
				source = fragmentedDiagnostic{source}
			}
			body := newDiagnosticBody(io.NopCloser(source), nil)
			defer body.Close()
			decoder := json.NewDecoder(body)
			for i := 0; i < 6; i++ {
				var v json.RawMessage
				if err := decoder.Decode(&v); err != nil {
					t.Fatal(i, err)
				}
				if !json.Valid(v) {
					t.Fatal(string(v))
				}
			}
			var v any
			if err := decoder.Decode(&v); err != io.EOF {
				t.Fatal(err)
			}
		})
	}
	// Cumulative bytes exceed the old finite-response cap; each record fits.
	line := `{"message":"` + strings.Repeat("x", maxDiagnosticRecord/2) + `"}` + "\n"
	body := newDiagnosticBody(io.NopCloser(strings.NewReader(strings.Repeat(line, 20))), nil)
	defer body.Close()
	decoder := json.NewDecoder(body)
	for range 20 {
		var v json.RawMessage
		if err := decoder.Decode(&v); err != nil {
			t.Fatal(err)
		}
	}
	var v any
	if err := decoder.Decode(&v); err != io.EOF {
		t.Fatal(err)
	}
}

func TestDiagnosticRecordBoundaryAndRejectedRemainder(t *testing.T) {
	for _, size := range []int{maxDiagnosticRecord, maxDiagnosticRecord + 1, 9 << 20} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			input := `{"message":"` + strings.Repeat("x", size-len(`{"message":""}`)) + `"}`
			source := &diagnosticTestBody{reader: strings.NewReader(input)}
			var cause error
			body := newDiagnosticBody(source, func(err error) { cause = err })
			if source.bytes.Load() != 0 {
				t.Fatal("eager read")
			}
			raw, err := io.ReadAll(body)
			if size == maxDiagnosticRecord {
				if err != nil || !json.Valid(bytes.TrimSpace(raw)) || len(raw) != size+1 {
					t.Fatal(len(raw), err)
				}
			} else {
				if apperror.Public(err).Code != "DiagnosticRecordTooLarge" || cause != err || len(raw) != 0 {
					t.Fatal(len(raw), err, cause)
				}
				if source.bytes.Load() > maxDiagnosticRecord+4096 {
					t.Fatal("rejected remainder drained", source.bytes.Load())
				}
				previous := source.bytes.Load()
				_, _ = io.Copy(io.Discard, body)
				if source.bytes.Load() != previous {
					t.Fatal("SDK cleanup drained rejected input")
				}
			}
			_ = body.Close()
			_ = body.Close()
			if source.closes.Load() != 1 {
				t.Fatal("duplicate close", source.closes.Load())
			}
		})
	}
}

func TestDiagnosticWhitespaceAndMalformedRecords(t *testing.T) {
	body := newDiagnosticBody(io.NopCloser(strings.NewReader(strings.Repeat(" \r\n\t", maxDiagnosticRecord)+`{"node":"ok"}`)), nil)
	raw, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil || string(raw) != "{\"node\":\"ok\"}\n" {
		t.Fatal(len(raw), err)
	}
	for _, raw := range []string{`{"a":]}`, `{"a":`, "\"unclosed", `{broken}`} {
		body := newDiagnosticBody(io.NopCloser(strings.NewReader(raw)), nil)
		if _, err := io.ReadAll(body); err == nil {
			t.Fatal("malformed record accepted", raw)
		}
		_ = body.Close()
	}
	failure := errors.New("read interrupted")
	body = newDiagnosticBody(io.NopCloser(io.MultiReader(strings.NewReader(`{"a":"`), adminFailedBody{err: failure})), nil)
	if _, err := io.ReadAll(body); !errors.Is(err, failure) {
		t.Fatal(err)
	}
}

func TestDiagnosticEOFClosesSourceBeforeSDKReconnect(t *testing.T) {
	source := &diagnosticTestBody{reader: strings.NewReader("{\"node\":\"done\"}\n")}
	body := newDiagnosticBody(source, nil)
	if _, err := io.ReadAll(body); err != nil {
		t.Fatal(err)
	}
	if source.closes.Load() != 1 {
		t.Fatal("SDK log reconnect would retain the completed response")
	}
	_ = body.Close()
	if source.closes.Load() != 1 {
		t.Fatal("duplicate close after EOF")
	}
}

func TestDiagnosticSDKLimitStopsWithoutReconnect(t *testing.T) {
	for _, trace := range []bool{false, true} {
		for _, length := range []int64{-1, 1, 9 << 20} {
			t.Run(fmt.Sprint(trace, "/", length), func(t *testing.T) {
				field := "ConsoleMsg"
				if trace {
					field = "msg"
				}
				raw := `{"` + field + `":"` + strings.Repeat("x", 9<<20) + `"}` + "\n"
				source := &diagnosticTestBody{reader: strings.NewReader(raw)}
				var calls atomic.Int32
				cfg := Config{Endpoint: diagnosticTestURL(), Credentials: credentials.NewStaticV4("test-key", "test-secret", "")}
				wire := roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls.Add(1)
					return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: length, Body: source}, nil
				})
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				var status int
				var cause error
				if trace {
					stream, err := OpenDiagnosticTrace(ctx, cfg, wire, false)
					if err != nil {
						t.Fatal(err)
					}
					defer stream.Close()
					select {
					case event, ok := <-stream.Events():
						if ok && event.Err == nil {
							t.Fatal("oversized trace decoded")
						}
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					cause, status = stream.Err(), stream.Status()
				} else {
					stream, err := OpenDiagnosticLogs(ctx, cfg, wire, 1)
					if err != nil {
						t.Fatal(err)
					}
					defer stream.Close()
					select {
					case _, ok := <-stream.Events():
						if ok {
							t.Fatal("oversized log decoded")
						}
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					cause, status = stream.Err(), stream.Status()
				}
				if apperror.Public(cause).Code != "DiagnosticRecordTooLarge" || status != http.StatusOK {
					t.Fatal(cause, status)
				}
				if calls.Load() != 1 || source.bytes.Load() > maxDiagnosticRecord+4096 || source.closes.Load() != 1 {
					t.Fatal("limit replayed or drained", calls.Load(), source.bytes.Load(), source.closes.Load())
				}
			})
		}
	}
}

func diagnosticTestURL() *url.URL { u, _ := url.Parse("http://fixture.invalid:9000"); return u }

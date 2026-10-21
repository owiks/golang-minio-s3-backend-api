package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/minio/madmin-go/v3"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
)

type countedAdminBody struct {
	reader io.Reader
	read   int
	closed int
}

func (b *countedAdminBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.read += n
	return n, err
}
func (b *countedAdminBody) Close() error { b.closed++; return nil }

func TestAdminFiniteResponsesBoundBeforeSDKAndNeverDrain(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		status     int
		limit      int64
	}{
		{"configuration", "/minio/admin/v3/get-config-kv", 200, maxAdminConfigResponse},
		{"history", "/minio/admin/v3/list-config-history-kv", 200, maxAdminHistory},
		{"new endpoint", "/minio/admin/v3/future", 200, maxAdminResponse},
		{"error", "/minio/admin/v3/accountinfo", 403, maxAdminError},
		{"failed stream", "/minio/admin/v3/trace", 502, maxAdminError},
	} {
		for _, length := range []string{"declared", "unknown", "understated"} {
			t.Run(tc.name+"/"+length, func(t *testing.T) {
				body := &countedAdminBody{reader: io.LimitReader(zeroReader{}, tc.limit+4096)}
				declared := int64(-1)
				if length == "declared" {
					declared = tc.limit + 4096
				} else if length == "understated" {
					declared = 1
				}
				response := &http.Response{StatusCode: tc.status, Header: http.Header{"X-Request-Id": {"original"}}, Body: body, ContentLength: declared}
				calls := 0
				wire := adminResponseTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response, nil })}
				r, _ := http.NewRequest("GET", "http://fixture.invalid"+tc.path, nil)
				got, err := wire.RoundTrip(r)
				if err != nil || got.StatusCode != tc.status || got.Header.Get("X-Request-Id") != "original" {
					t.Fatal("original response metadata or SDK retry boundary changed", err)
				}
				_, err = io.ReadAll(got.Body)
				if apperror.Public(err).Code != "AdminResponseTooLarge" || calls != 1 || body.closed != 1 {
					t.Fatal("missing size error or resource cleanup", err, calls, body.closed)
				}
				if length == "declared" && body.read != 0 || int64(body.read) > tc.limit+1 {
					t.Fatal("read beyond boundary", body.read, tc.limit)
				}
				_, _ = io.Copy(io.Discard, got.Body) // Mirrors the SDK's deferred closeResponse.
				_ = got.Body.Close()
				if body.closed != 1 || int64(body.read) > tc.limit+1 {
					t.Fatal("SDK cleanup drained or closed upstream twice")
				}
			})
		}
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestAdminFiniteResponsePreservesExactBoundaryAndFailures(t *testing.T) {
	for _, size := range []int{0, 27, maxAdminConfigResponse} {
		payload := bytes.Repeat([]byte("x"), size)
		body := &countedAdminBody{reader: bytes.NewReader(payload)}
		r, _ := http.NewRequest("GET", "http://fixture.invalid/minio/admin/v3/get-config-kv", nil)
		response := &http.Response{StatusCode: 200, Body: body, ContentLength: -1}
		if err := boundAdminResponse(r.Method, r.URL.Path, response); err != nil {
			t.Fatal("valid boundary rejected", err)
		}
		got, err := io.ReadAll(response.Body)
		if err != nil || !bytes.Equal(got, payload) || body.closed != 1 {
			t.Fatal("original bytes lost", err)
		}
	}
	for _, failure := range []error{io.ErrUnexpectedEOF, context.Canceled, context.DeadlineExceeded} {
		// A complete JSON prefix followed by a read failure must not be accepted.
		body := &countedAdminBody{reader: io.MultiReader(strings.NewReader(`{}`), adminFailedBody{err: failure})}
		client := adminFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: body, ContentLength: -1}, nil
		}))
		_, err := client.ListUsers(context.Background())
		if !errors.Is(err, failure) || body.closed != 1 {
			t.Fatal("partial decode masked read failure", err)
		}
	}
}

func TestAdminSDKRejectsOversizedBodiesWithoutRetryOrDecryption(t *testing.T) {
	for _, kind := range []string{"configuration", "history", "JSON", "HTTP error", "command success", "command error"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			var body *countedAdminBody
			ctx, trace := telemetry.Start(context.Background(), "GET admin")
			client := adminFixture(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				status := 200
				if strings.Contains(kind, "error") {
					status = 502
				}
				response := &http.Response{StatusCode: status, Header: make(http.Header), ContentLength: -1}
				limit := adminResponseLimit(r.Method, r.URL.Path, response.StatusCode)
				// Invalid encryption/JSON would fail differently if passed to the SDK.
				body = &countedAdminBody{reader: io.LimitReader(zeroReader{}, limit+4096)}
				response.Body = body
				return response, nil
			}))
			var err error
			switch kind {
			case "configuration":
				_, err = client.GetConfigKVWithOptions(ctx, "api", madmin.KVOptions{Env: true})
			case "history":
				_, err = client.ListConfigHistoryKV(ctx, 100)
			case "command success", "command error":
				err = client.AddUser(ctx, "fixture-user", "fixture-secret")
			default:
				_, err = client.ListUsers(ctx)
			}
			if apperror.Public(err).Code != "AdminResponseTooLarge" || calls != 1 || body.closed != 1 {
				t.Fatal("size error masked, retried or leaked", err, calls, body.closed)
			}
			if trace.UpstreamSnapshot().Attempts != 1 || trace.UpstreamSnapshot().Status == 400 {
				t.Fatal("synthetic response counted as upstream", trace.UpstreamSnapshot())
			}
		})
	}
}

func TestAdminEncryptedConfigAndHistoryStillDecode(t *testing.T) {
	for _, kind := range []string{"configuration", "history"} {
		t.Run(kind, func(t *testing.T) {
			plaintext := []byte(`[{"restoreId":"fixture","data":"api requests_max=10"}]`)
			if kind == "configuration" {
				plaintext = bytes.Repeat([]byte("x"), 256<<10)
			}
			ciphertext, err := madmin.EncryptData("fixture-secret", plaintext)
			if err != nil {
				t.Fatal(err)
			}
			body := &countedAdminBody{reader: bytes.NewReader(ciphertext)}
			client := adminFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: body, ContentLength: int64(len(ciphertext)), Header: make(http.Header)}, nil
			}))
			if kind == "configuration" {
				got, err := client.GetConfigKVWithOptions(context.Background(), "api", madmin.KVOptions{Env: true})
				if err != nil || !bytes.Equal(got, plaintext) {
					t.Fatal("encrypted config boundary changed", err)
				}
			} else {
				got, err := client.ListConfigHistoryKV(context.Background(), 100)
				if err != nil || len(got) != 1 || got[0].Data != "api requests_max=10" {
					t.Fatal("encrypted history changed", err)
				}
			}
			if body.closed != 1 {
				t.Fatal("upstream not closed exactly once")
			}
		})
	}
}

func TestAdminOnlySuccessfulDiagnosticSubscriptionsRemainLazy(t *testing.T) {
	for _, path := range []string{"log", "trace"} {
		const event = "{\"node\":\"first\"}\n"
		body := &countedAdminBody{reader: strings.NewReader(event)}
		client := adminFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: body, ContentLength: maxAdminResponse + 1, Header: make(http.Header)}, nil
		}))
		response, err := client.ExecuteMethod(context.Background(), "GET", madmin.RequestData{RelPath: "/v3/" + path})
		if err != nil || body.read != 0 || body.closed != 0 {
			t.Fatal("diagnostic stream buffered or closed", path, err, body)
		}
		got, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || string(got) != event || body.closed != 1 {
			t.Fatal("diagnostic stream changed", err)
		}
	}
}

package storage

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
)

func TestSDKObjectAndPartPutRejectUnexpectedSuccessBody(t *testing.T) {
	for _, part := range []bool{false, true} {
		for _, payload := range []string{"", " \r\n", `<Error><Code>AccessDenied</Code><Message>PRIVATE-UPSTREAM</Message></Error>`, `<Error><Code>InternalError</Code></Error>`, `<UnexpectedSuccess/>`, `PRIVATE-UPSTREAM`} {
			t.Run(strings.Join([]string{map[bool]string{true: "part", false: "object"}[part], payload}, "/"), func(t *testing.T) {
				calls := 0
				body := &countedAdminBody{reader: strings.NewReader(payload)}
				client := s3Fixture(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.Method != http.MethodPut {
						t.Error("unexpected mutation", r.Method)
					}
					if r.Body != nil {
						_, _ = io.Copy(io.Discard, r.Body)
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Etag": {`"untrusted-etag"`}}, Body: body}, nil
				}))
				var err error
				if part {
					_, err = (minio.Core{Client: client}).PutObjectPart(context.Background(), "fixture-bucket", "file.txt", "fixture-upload", 1, strings.NewReader("body"), 4, minio.PutObjectPartOptions{})
				} else {
					_, err = client.PutObject(context.Background(), "fixture-bucket", "file.txt", strings.NewReader("body"), 4, minio.PutObjectOptions{DisableMultipart: true})
				}
				if strings.TrimSpace(payload) == "" {
					if err != nil {
						t.Fatal("normal PUT rejected", err)
					}
				} else if err == nil || apperror.Public(err).Code != "S3WriteUnconfirmed" || strings.Contains(err.Error(), "PRIVATE") {
					t.Fatal("unexpected PUT body accepted or exposed", err)
				}
				if calls != 1 || body.closed != 1 {
					t.Fatal("write repeated or response not closed", calls, body.closed)
				}
			})
		}
	}
}

func TestS3WriteResultGuardDoesNotInspectOtherOperations(t *testing.T) {
	for _, tc := range []struct {
		method, raw string
		headers     http.Header
	}{
		{"GET", "/fixture-bucket/file.txt", nil},
		{"PUT", "/fixture-bucket?policy", nil},
		{"PUT", "/fixture-bucket/", nil},
		{"POST", "/fixture-bucket/file.txt?uploadId=fixture", nil},
		{"PUT", "/fixture-bucket/file.txt?future=true", nil},
		{"PUT", "/fixture-bucket/file.txt?partNumber=1&uploadId=fixture&future=true", nil},
		{"PUT", "/fixture-bucket/file.txt?partNumber=1&partNumber=2&uploadId=fixture", nil},
		{"PUT", "/fixture-bucket/file.txt?tagging", nil},
		{"PUT", "/fixture-bucket/file.txt", http.Header{"X-Amz-Copy-Source": {"/source/key"}}},
		{"PUT", "/fixture-bucket/file.txt?uploadId=fixture&partNumber=1", http.Header{"X-Amz-Copy-Source": {"/source/key"}}},
	} {
		t.Run(tc.method+tc.raw, func(t *testing.T) {
			u, err := url.Parse("http://fixture.invalid" + tc.raw)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateS3WriteResult(tc.method, u, tc.headers, nil, 200, []byte(`<Error><Code>InternalError</Code></Error>`)); err != nil {
				t.Fatal("guard crossed the ordinary PUT boundary", err)
			}
		})
	}
}

func TestSDKObjectAndPartPutRequireETagConfirmation(t *testing.T) {
	for _, part := range []bool{false, true} {
		for _, etag := range []string{"", `""`, "  "} {
			t.Run(map[bool]string{true: "part", false: "object"}[part]+"/"+etag, func(t *testing.T) {
				calls := 0
				body := &countedAdminBody{reader: strings.NewReader("")}
				client := s3Fixture(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.Body != nil {
						_, _ = io.Copy(io.Discard, r.Body)
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Etag": {etag}}, Body: body}, nil
				}))
				var err error
				if part {
					_, err = (minio.Core{Client: client}).PutObjectPart(context.Background(), "fixture-bucket", "file.txt", "fixture-upload", 1, strings.NewReader("body"), 4, minio.PutObjectPartOptions{})
				} else {
					_, err = client.PutObject(context.Background(), "fixture-bucket", "file.txt", strings.NewReader("body"), 4, minio.PutObjectOptions{DisableMultipart: true})
				}
				if err == nil || apperror.Public(err).Code != "S3WriteUnconfirmed" || calls != 1 || body.closed != 1 {
					t.Fatal("unconfirmed PUT accepted, retried or not closed", calls, body.closed, err)
				}
			})
		}
	}
}

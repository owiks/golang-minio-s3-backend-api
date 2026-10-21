package uploads

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/staging"
	"minio-web/api/src/services/uploads/integrity"
)

func sha256Spec(payload string) integrity.Spec {
	h := sha256.Sum256([]byte(payload))
	return integrity.Spec{Algorithm: "SHA256", Value: base64.StdEncoding.EncodeToString(h[:])}
}

func TestChecksumUploadChecksBeforeMutationAndReleasesBudget(t *testing.T) {
	for _, scenario := range []string{"correct", "empty", "mismatch", "invalid", "composite", "short", "surplus", "source error", "budget full", "canceled", "native mismatch", "no budget"} {
		t.Run(scenario, func(t *testing.T) {
			payload := "body"
			if scenario == "empty" {
				payload = ""
			}
			spec := sha256Spec(payload)
			size := int64(len(payload))
			var reader io.Reader = strings.NewReader(payload)
			budget := staging.New(8)
			defer budget.Close()
			client := &conditionalDestination{lookupErr: minio.ErrorResponse{Code: "NoSuchKey"}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantCode, wantWrites := "", 1
			switch scenario {
			case "mismatch":
				spec = sha256Spec("different")
				wantCode, wantWrites = "ChecksumMismatch", 0
			case "invalid":
				spec.Value = "invalid"
				wantCode, wantWrites = "InvalidChecksum", 0
			case "composite":
				spec.Type = integrity.Composite
				spec.Value += "-1"
				wantCode, wantWrites = "UnsupportedChecksumMode", 0
			case "short":
				size++
				wantCode, wantWrites = "IncompleteUpload", 0
			case "surplus":
				size--
				wantCode, wantWrites = "IncompleteUpload", 0
			case "source error":
				reader = checksumErrorReader{}
				wantCode, wantWrites = "IncompleteUpload", 0
			case "budget full":
				budget = staging.New(1)
				wantCode, wantWrites = "TemporaryStorageFull", 0
			case "canceled":
				cancel()
				wantWrites = 0
			case "native mismatch":
				client.writeErr = minio.ErrorResponse{Code: "BadDigest"}
				wantCode = "ChecksumMismatch"
			case "no budget":
				budget = nil
				wantCode, wantWrites = "ChecksumStagingUnavailable", 0
			}
			value, err := WriteFileWithChecksum(ctx, budget, client, "bucket", "file.txt", "absent", "text/plain", size, reader, &spec)
			if client.writes != wantWrites {
				t.Fatal("unexpected write or repeat", client.writes, wantWrites)
			}
			if budget != nil && budget.Used() != 0 {
				t.Fatal("temporary bytes leaked", budget.Used())
			}
			if scenario == "canceled" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				return
			}
			if wantCode != "" {
				if err == nil || apperror.Public(err).Code != wantCode {
					t.Fatal("incorrect error", err, wantCode)
				}
				return
			}
			if err != nil || !value.OK || value.Checksum.Verification != "api_verified" || value.Checksum.Expected.Value != spec.Value || len(value.Checksum.Storage) != 0 {
				t.Fatal(value, err)
			}
			if client.body != payload || client.headers.Get("If-None-Match") != "*" || client.lookups != 2 {
				t.Fatal("staging changed destination contract", client)
			}
		})
	}
}

type checksumErrorReader struct{}

func (checksumErrorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestChecksumUploadLegacyReceiptRemainsCompatible(t *testing.T) {
	client := &conditionalDestination{lookupErr: minio.ErrorResponse{Code: "NoSuchKey"}}
	value, err := WriteFileWithChecksum(context.Background(), nil, client, "bucket", "file.txt", "absent", "text/plain", 4, strings.NewReader("body"), nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil || string(data) != `{"ok":true,"etag":"written","versionId":"","size":4}` || client.lookups != 1 || client.writes != 1 {
		t.Fatal("legacy upload changed", string(data), err)
	}
}

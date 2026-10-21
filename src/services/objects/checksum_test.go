package objects

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/result"
	"minio-web/api/src/services/telemetry"
)

type checksumClient struct {
	t         *testing.T
	info      minio.ObjectInfo
	statCalls int
}

func (s *checksumClient) StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error) {
	s.statCalls++
	return s.info, nil
}
func (s *checksumClient) GetObject(context.Context, string, string, minio.GetObjectOptions) (*minio.Object, error) {
	s.t.Error("unexpected object read")
	return nil, io.ErrUnexpectedEOF
}

func checksumFixture(t *testing.T, body []byte, checksums []StoredChecksum) (*ChecksumRun, *checksumClient) {
	t.Helper()
	info := minio.ObjectInfo{Size: int64(len(body)), ETag: "etag", VersionID: "version", LastModified: time.Unix(1700000000, 0).UTC()}
	ctx, cancel := context.WithCancel(context.Background())
	client := &checksumClient{t: t, info: info}
	run := &ChecksumRun{ctx: ctx, cancel: cancel, client: client, bucket: "bucket", key: "file", calculate: true, summary: ChecksumInfo{Checksums: slices.Clone(checksums)}, info: info, first: bytes.Clone(body), n: len(body), readErr: io.EOF}
	t.Cleanup(run.Close)
	return run, client
}

func checksumResult(t *testing.T, run *ChecksumRun, client *checksumClient, expected string) (*ChecksumResult, *telemetry.Trace) {
	t.Helper()
	ctx, trace := telemetry.Start(context.Background(), "checksum")
	run.expected = expected
	var value *ChecksumResult
	err := run.Stream(ctx, func(event result.StreamEvent[ChecksumResult]) bool {
		if event.Error != nil {
			t.Errorf("checksum failed: %s", event.Error.Code)
		}
		if event.Result != nil {
			if value != nil {
				t.Error("multiple terminal results")
			}
			value = event.Result
		}
		return true
	})
	if err != nil || value == nil || client.statCalls != 1 {
		t.Fatal("missing result or final object identity check", value, client.statCalls, err)
	}
	return value, trace
}

func TestChecksumExpectedHEXIgnoresCase(t *testing.T) {
	body := []byte("checksum integrity")
	sum := sha256.Sum256(body)
	lower := hex.EncodeToString(sum[:])
	wrong := "0" + lower[1:]
	if wrong == lower {
		wrong = "f" + lower[1:]
	}
	for _, tc := range []struct{ name, expected, status string }{
		{"lower", lower, "matched"},
		{"upper", strings.ToUpper(lower), "matched"},
		{"mixed", strings.ToUpper(lower[:32]) + lower[32:], "matched"},
		{"different upper", strings.ToUpper(wrong), "mismatch"},
		{"absent", "", "calculated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run, client := checksumFixture(t, body, nil)
			value, trace := checksumResult(t, run, client, tc.expected)
			if value.Status != tc.status || value.SHA256 != lower || value.Bytes != int64(len(body)) || value.Version != "version" {
				t.Fatal("wrong checksum decision", value)
			}
			if tc.expected != "" && (len(value.Comparisons) != 1 || value.Comparisons[0].Expected != tc.expected || value.Comparisons[0].Computed != lower || value.Comparisons[0].Source != "expected") {
				t.Fatal("comparison changed the caller's HEX or response format", value.Comparisons)
			}
			if (trace.Code == "ChecksumMismatch") != (tc.status == "mismatch") {
				t.Fatal("checksum telemetry contradicts the decision", trace.Result, trace.Code)
			}
		})
	}
}

func TestChecksumEmptyObjectAcceptsUpperHEX(t *testing.T) {
	run, client := checksumFixture(t, nil, nil)
	sum := sha256.Sum256(nil)
	value, _ := checksumResult(t, run, client, strings.ToUpper(hex.EncodeToString(sum[:])))
	if value.Status != "matched" || value.Bytes != 0 {
		t.Fatal("empty object checksum did not match", value)
	}
}

func TestPrepareChecksumValidatesHEXBeforeStorage(t *testing.T) {
	lower := strings.Repeat("abcdef01", 8)
	for _, tc := range []struct {
		name, expected string
		valid          bool
	}{
		{"lower", lower, true},
		{"upper", strings.ToUpper(lower), true},
		{"mixed", strings.ToUpper(lower[:32]) + lower[32:], true},
		{"absent", "", true},
		{"short", lower[:62], false},
		{"long", lower + "00", false},
		{"odd", lower[:63], false},
		{"non hex", strings.Repeat("G", 64), false},
		{"space", " " + lower, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &checksumClient{t: t, info: minio.ObjectInfo{Size: 3, ETag: "etag"}}
			run, err := PrepareChecksum(context.Background(), client, "bucket", "file", tc.expected, false)
			if tc.valid {
				if err != nil || run == nil || client.statCalls != 1 {
					t.Fatal("valid HEX was rejected", err, client.statCalls)
				}
				run.Close()
			} else if err == nil || apperror.Public(err).Code != "InvalidExpectedChecksum" || run != nil || client.statCalls != 0 {
				t.Fatal("invalid HEX reached storage", err, client.statCalls)
			}
		})
	}
}

func TestChecksumStorageBase64RemainsCaseSensitive(t *testing.T) {
	body := []byte("checksum integrity")
	sum := sha256.Sum256(body)
	encoded := base64.StdEncoding.EncodeToString(sum[:])
	changed := []byte(encoded)
	for i, b := range changed {
		if b >= 'A' && b <= 'Z' {
			changed[i] += 'a' - 'A'
			break
		}
		if b >= 'a' && b <= 'z' {
			changed[i] -= 'a' - 'A'
			break
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(string(changed))
	if err != nil || bytes.Equal(decoded, sum[:]) {
		t.Fatal("invalid Base64 regression fixture")
	}
	for _, tc := range []struct{ name, stored, status string }{
		{"unchanged", encoded, "matched"},
		{"changed case", string(changed), "mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run, client := checksumFixture(t, body, []StoredChecksum{{Algorithm: "SHA256", Value: tc.stored, Type: "FULL_OBJECT"}})
			value, _ := checksumResult(t, run, client, "")
			if value.Status != tc.status || len(value.Comparisons) != 1 || value.Comparisons[0].Computed != encoded || value.Comparisons[0].Expected != tc.stored {
				t.Fatal("storage Base64 was normalized or ignored", value)
			}
		})
	}
}

func TestChecksumSkipsNonFullAndUnsupportedStorageReferences(t *testing.T) {
	for _, tc := range []StoredChecksum{
		{Algorithm: "SHA256", Value: "checksum-2", Type: "COMPOSITE"},
		{Algorithm: "SHA256", Value: "checksum", Type: "UNKNOWN"},
		{Algorithm: "XXHASH64", Value: "checksum", Type: "FULL_OBJECT"},
		{Algorithm: "unsupported", Value: "checksum", Type: "FULL_OBJECT"},
	} {
		t.Run(tc.Algorithm+"/"+tc.Type, func(t *testing.T) {
			run, client := checksumFixture(t, []byte("body"), []StoredChecksum{tc})
			value, _ := checksumResult(t, run, client, "")
			if value.Status != "calculated" || len(value.Comparisons) != 0 {
				t.Fatal("unsupported checksum was treated as verified", value)
			}
		})
	}
}

func TestChecksumAlgorithmInstancesAreIndependentAcrossRuns(t *testing.T) {
	body := []byte("checksum integrity")
	checksums := []StoredChecksum{}
	for _, algorithm := range []struct {
		name string
		kind minio.ChecksumType
	}{
		{"SHA256", minio.ChecksumSHA256}, {"SHA1", minio.ChecksumSHA1},
		{"CRC32", minio.ChecksumCRC32}, {"CRC32C", minio.ChecksumCRC32C},
		{"CRC64NVME", minio.ChecksumCRC64NVME}, {"MD5", minio.ChecksumMD5}, {"SHA512", minio.ChecksumSHA512},
	} {
		h := algorithm.kind.Hasher()
		_, _ = h.Write(body)
		checksums = append(checksums, StoredChecksum{Algorithm: algorithm.name, Value: base64.StdEncoding.EncodeToString(h.Sum(nil)), Type: "FULL_OBJECT"})
	}
	sum := sha256.Sum256(body)
	expected := hex.EncodeToString(sum[:])
	var work sync.WaitGroup
	for range 32 {
		work.Go(func() {
			run, client := checksumFixture(t, body, checksums)
			value, _ := checksumResult(t, run, client, expected)
			if value.Status != "matched" || len(value.Comparisons) != 8 {
				t.Error("algorithm set changed or hash state was shared", value)
			}
		})
	}
	work.Wait()
}

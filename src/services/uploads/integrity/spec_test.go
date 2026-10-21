package integrity

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
)

func TestChecksumValidationAndExactBody(t *testing.T) {
	for _, algorithm := range []string{"MD5", "SHA1", "SHA256", "CRC32", "CRC32C"} {
		for _, payload := range []string{"", "checksum value"} {
			t.Run(algorithm+"/"+payload, func(t *testing.T) {
				h, err := NewHasher(algorithm)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.WriteString(h, payload)
				expected, err := Normalize(Spec{Algorithm: strings.ToLower(algorithm), Value: base64.StdEncoding.EncodeToString(h.Sum(nil))}, Object)
				if err != nil || Header(expected) == "" || SDKType(expected) == 0 {
					t.Fatal(expected, err)
				}
				computed, err := Hash(context.Background(), strings.NewReader(payload), int64(len(payload)), expected)
				if err != nil || Compare(expected, computed) != nil || Verify(expected, h.Sum(nil)) != nil {
					t.Fatal(computed, err)
				}
			})
		}
	}
	base := Spec{Algorithm: "SHA256", Value: base64.StdEncoding.EncodeToString(make([]byte, 32))}
	cases := []struct {
		name   string
		change func(*Spec)
		scope  string
	}{
		{"unknown algorithm", func(s *Spec) { s.Algorithm = "SHA512" }, Object},
		{"wrong encoding", func(s *Spec) { s.Encoding = "hex" }, Object},
		{"invalid base64", func(s *Spec) { s.Value = "bad" }, Object},
		{"incorrect digest length", func(s *Spec) { s.Value = "AAAA" }, Object},
		{"scope mismatch", func(s *Spec) { s.Scope = Part }, Object},
		{"part composite", func(s *Spec) { s.Type = Composite; s.Value += "-2" }, Part},
		{"missing count", func(s *Spec) { s.Type = Composite }, Object},
		{"invalid count", func(s *Spec) { s.Type = Composite; s.Value += "-0" }, Object},
		{"too many parts", func(s *Spec) { s.Type = Composite; s.Value += "-10001" }, Object},
		{"noncanonical count", func(s *Spec) { s.Type = Composite; s.Value += "-01" }, Object},
		{"new line", func(s *Spec) { s.Value += "\n" }, Object},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := base
			tc.change(&value)
			if _, err := Normalize(value, tc.scope); err == nil {
				t.Fatal("accepted invalid checksum", value)
			}
		})
	}
	base.Type, base.Value = Composite, base.Value+"-2"
	if _, err := Normalize(base, Object); err != nil {
		t.Fatal("valid composite rejected", err)
	}
	if _, err := Hash(context.Background(), strings.NewReader(""), 0, base); apperror.Public(err).Code != "UnsupportedChecksumMode" {
		t.Fatal(err)
	}
	base.Type = FullObject
	if err := Compare(base, Spec{Algorithm: base.Algorithm, Type: Composite}); err == nil {
		t.Fatal("compared different modes")
	}
}

func TestHashRejectsTruncationSurplusAndCancellation(t *testing.T) {
	spec := Spec{Algorithm: "SHA256", Type: FullObject, Scope: Object, Encoding: "base64"}
	for _, size := range []int64{0, 2, 8} {
		if _, err := Hash(context.Background(), strings.NewReader("body"), size, spec); err == nil {
			t.Fatal("wrong byte count accepted", size)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Hash(ctx, strings.NewReader("body"), 4, spec); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}

func TestUploadEvidenceNeverInventsStorageChecksumOrMode(t *testing.T) {
	spec, _ := Normalize(Spec{Algorithm: "SHA256", Value: base64.StdEncoding.EncodeToString(make([]byte, 32))}, Object)
	value := FromUpload(minio.UploadInfo{ETag: strings.Repeat("0", 32)}, &spec, &spec, "CONTENT_MD5")
	if value.Verification != "api_verified" || len(value.Storage) != 0 {
		t.Fatal("invented stored checksum", value)
	}
	value = FromUpload(minio.UploadInfo{ChecksumSHA256: spec.Value}, &spec, &spec, "CONTENT_MD5")
	if value.Verification != "api_verified" || len(value.Storage) != 1 || value.Storage[0].Type != "" {
		t.Fatal("invented mode", value)
	}
	value = FromUpload(minio.UploadInfo{ChecksumSHA256: spec.Value, ChecksumMode: FullObject}, &spec, &spec, "")
	if value.Verification != "storage_confirmed" {
		t.Fatal(value)
	}
	changed := spec
	changed.Value = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
	value = FromUpload(minio.UploadInfo{ChecksumSHA256: changed.Value, ChecksumMode: FullObject}, &spec, &spec, "")
	if value.Verification != "storage_mismatch" {
		t.Fatal(value)
	}
	spec.Value = "caller mutated"
	if value.Expected.Value == spec.Value || value.Computed.Value == spec.Value {
		t.Fatal("evidence aliases caller state")
	}
}

// Package integrity defines checksum values shared by upload transports. A
// checksum of one part and a composite checksum of an object are distinct values.
package integrity

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"hash"
	"hash/crc32"
	"strconv"
	"strings"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

const (
	Object     = "OBJECT"
	Part       = "PART"
	FullObject = "FULL_OBJECT"
	Composite  = "COMPOSITE"
)

// Spec is a detached value. Value is canonical padded standard base64; composite
// values additionally carry the number of parts as a decimal -N suffix.
type Spec struct {
	Algorithm string `json:"algorithm"`
	Encoding  string `json:"encoding"`
	Value     string `json:"value"`
	Type      string `json:"type"`
	Scope     string `json:"scope"`
}

func invalid() error {
	return apperror.New(400, "InvalidChecksum", messages.UploadsInvalidChecksum)
}

func UnsupportedMode() error {
	return apperror.New(400, "UnsupportedChecksumMode", messages.UploadsUnsupportedChecksumMode)
}

// Normalize validates an explicitly supplied value before any body is consumed
// or upload command is sent. It does not assert server support for the algorithm.
func Normalize(spec Spec, scope string) (Spec, error) {
	// Bound all parsing, including base64 allocation and numeric suffix parsing,
	// independently of the surrounding HTTP header/JSON limit.
	if len(spec.Algorithm) > 16 || len(spec.Value) > 64 || len(spec.Type) > 16 || len(spec.Scope) > 8 {
		return Spec{}, invalid()
	}
	spec.Algorithm = strings.ToUpper(spec.Algorithm)
	if spec.Encoding == "" {
		spec.Encoding = "base64"
	}
	if spec.Type == "" {
		spec.Type = FullObject
	}
	if spec.Scope == "" {
		spec.Scope = scope
	}
	if spec.Encoding != "base64" || spec.Scope != scope || (scope != Object && scope != Part) || (spec.Type != FullObject && spec.Type != Composite) || (scope == Part && spec.Type != FullObject) {
		return Spec{}, invalid()
	}
	h, err := NewHasher(spec.Algorithm)
	if err != nil {
		return Spec{}, err
	}
	encoded := spec.Value
	if spec.Type == Composite {
		cut := strings.LastIndexByte(encoded, '-')
		if cut < 1 {
			return Spec{}, invalid()
		}
		n, e := strconv.Atoi(encoded[cut+1:])
		if e != nil || n < 1 || n > 10000 || strconv.Itoa(n) != encoded[cut+1:] {
			return Spec{}, invalid()
		}
		encoded = encoded[:cut]
	}
	value, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(value) != h.Size() || base64.StdEncoding.EncodeToString(value) != encoded {
		return Spec{}, invalid()
	}
	return spec, nil
}

// NewHasher supports only explicitly reviewed full-byte algorithms. MD5/SHA1 are
// interoperability checks, not signatures or protection against malicious input.
func NewHasher(algorithm string) (hash.Hash, error) {
	switch algorithm {
	case "MD5":
		return md5.New(), nil
	case "SHA1":
		return sha1.New(), nil
	case "SHA256":
		return sha256.New(), nil
	case "CRC32":
		return crc32.NewIEEE(), nil
	case "CRC32C":
		return crc32.New(crc32.MakeTable(crc32.Castagnoli)), nil
	default:
		return nil, apperror.New(400, "UnsupportedChecksumAlgorithm", messages.UploadsUnsupportedChecksumAlgorithm)
	}
}

func Header(spec Spec) string {
	if spec.Algorithm == "MD5" {
		return "Content-MD5"
	}
	return "x-amz-checksum-" + strings.ToLower(spec.Algorithm)
}

// SDKType returns the base algorithm. Operation-specific code must separately
// choose FULL_OBJECT/COMPOSITE; this helper never grants support for a mode.
func SDKType(spec Spec) minio.ChecksumType {
	switch spec.Algorithm {
	case "MD5":
		return minio.ChecksumMD5
	case "SHA1":
		return minio.ChecksumSHA1
	case "SHA256":
		return minio.ChecksumSHA256
	case "CRC32":
		return minio.ChecksumCRC32
	case "CRC32C":
		return minio.ChecksumCRC32C
	default:
		return 0
	}
}

// Value preserves scope/mode while replacing the checksum bytes.
func Value(spec Spec, computed []byte) Spec {
	spec.Value = base64.StdEncoding.EncodeToString(computed)
	return spec
}

func Verify(spec Spec, computed []byte) error {
	if spec.Type != FullObject {
		return UnsupportedMode()
	}
	value, err := base64.StdEncoding.DecodeString(spec.Value)
	if err != nil || subtle.ConstantTimeCompare(value, computed) != 1 {
		return apperror.New(400, "ChecksumMismatch", messages.UploadsChecksumMismatch)
	}
	return nil
}

// Compare checks values only when their algorithm, scope and mode agree.
// Composite values may be compared here; they must never be hashed as raw bytes.
func Compare(expected, actual Spec) error {
	if expected.Algorithm != actual.Algorithm || expected.Type != actual.Type || expected.Scope != actual.Scope || expected.Encoding != actual.Encoding {
		return UnsupportedMode()
	}
	if subtle.ConstantTimeCompare([]byte(expected.Value), []byte(actual.Value)) != 1 {
		return apperror.New(400, "ChecksumMismatch", messages.UploadsChecksumMismatch)
	}
	return nil
}

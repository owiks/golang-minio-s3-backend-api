package objects

import (
	"strings"

	"github.com/minio/minio-go/v7"
)

type StoredChecksum struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
	Type      string `json:"type"`
}
type ChecksumInfo struct {
	Size      int64            `json:"size"`
	ETag      string           `json:"etag"`
	Version   string           `json:"version"`
	Checksums []StoredChecksum `json:"checksums"`
	Warning   string           `json:"warning,omitempty"`
}

// Return algorithm values without exposing a shared registry or hash instance.
func checksumAlgorithm(name string) (minio.ChecksumType, bool) {
	switch name {
	case "SHA256":
		return minio.ChecksumSHA256, true
	case "SHA1":
		return minio.ChecksumSHA1, true
	case "CRC32":
		return minio.ChecksumCRC32, true
	case "CRC32C":
		return minio.ChecksumCRC32C, true
	case "CRC64NVME":
		return minio.ChecksumCRC64NVME, true
	case "MD5":
		return minio.ChecksumMD5, true
	case "SHA512":
		return minio.ChecksumSHA512, true
	default:
		return minio.ChecksumNone, false
	}
}

func AvailableChecksums(info minio.ObjectInfo) []StoredChecksum {
	result := []StoredChecksum{}
	values := map[string]string{"SHA256": info.ChecksumSHA256, "SHA1": info.ChecksumSHA1, "CRC32": info.ChecksumCRC32, "CRC32C": info.ChecksumCRC32C, "CRC64NVME": info.ChecksumCRC64NVME, "MD5": info.ChecksumMD5, "SHA512": info.ChecksumSHA512, "XXHASH64": info.ChecksumXXHash64, "XXHASH3": info.ChecksumXXHash3, "XXHASH128": info.ChecksumXXHash128}
	for _, name := range []string{"SHA256", "SHA1", "CRC32", "CRC32C", "CRC64NVME", "MD5", "SHA512", "XXHASH64", "XXHASH3", "XXHASH128"} {
		value := values[name]
		if value == "" {
			continue
		}
		kind := info.ChecksumMode
		if kind != "FULL_OBJECT" && kind != "COMPOSITE" {
			kind = "UNKNOWN"
		}
		if strings.Contains(value, "-") {
			kind = "COMPOSITE"
		}
		result = append(result, StoredChecksum{Algorithm: name, Value: value, Type: kind})
	}
	return result
}

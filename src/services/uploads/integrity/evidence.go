package integrity

import "github.com/minio/minio-go/v7"

// Evidence distinguishes a value claimed by the caller, a local byte check and
// checksums actually present in the upload response. Storage is not a readback
// assertion and is never synthesized from an ETag or a local calculation.
type Evidence struct {
	Expected          *Spec  `json:"expected,omitempty"`
	Computed          *Spec  `json:"computed,omitempty"`
	Storage           []Spec `json:"storage,omitempty"`
	StorageSource     string `json:"storageSource,omitempty"`
	Verification      string `json:"verification"`
	RequestProtection string `json:"requestProtection,omitempty"`
}

func FromUpload(info minio.UploadInfo, expected, computed *Spec, protection string) *Evidence {
	e := &Evidence{Verification: "not_requested", RequestProtection: protection}
	if expected != nil {
		value := *expected
		e.Expected = &value
	}
	if computed != nil {
		value := *computed
		e.Computed = &value
		e.Verification = "api_verified"
	}
	reference := expected
	if reference == nil {
		reference = computed
	}
	// Mode is not inferred from object size, ETag or checksum shape. A server
	// which omits it does not provide sufficient evidence for mode comparison.
	values := []struct{ algorithm, value string }{
		{"CRC32", info.ChecksumCRC32}, {"CRC32C", info.ChecksumCRC32C},
		{"SHA1", info.ChecksumSHA1}, {"SHA256", info.ChecksumSHA256}, {"MD5", info.ChecksumMD5},
	}
	for _, item := range values {
		if item.value == "" {
			continue
		}
		mode := info.ChecksumMode
		if mode != FullObject && mode != Composite {
			mode = FullObject
		}
		value, err := Normalize(Spec{Algorithm: item.algorithm, Value: item.value, Type: mode}, Object)
		if err != nil && info.ChecksumMode == "" {
			value, err = Normalize(Spec{Algorithm: item.algorithm, Value: item.value, Type: Composite}, Object)
		}
		if err != nil {
			continue
		}
		if info.ChecksumMode != FullObject && info.ChecksumMode != Composite {
			value.Type = ""
		}
		e.Storage = append(e.Storage, value)
		if reference != nil && value.Algorithm == reference.Algorithm && value.Type == reference.Type && value.Scope == reference.Scope {
			if value.Value == reference.Value {
				e.Verification = "storage_confirmed"
			} else {
				e.Verification = "storage_mismatch"
			}
		}
	}
	if len(e.Storage) > 0 {
		e.StorageSource = "upload_response"
	}
	return e
}

package apperror

import (
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/tests/testmessage"
)

func TestNativeChecksumErrorsRemainSpecificAndSanitized(t *testing.T) {
	for _, tc := range []struct{ native, public string }{
		{"BadDigest", "ChecksumMismatch"},
		{"XAmzContentSHA256Mismatch", "ChecksumMismatch"},
		{"ChecksumMismatch", "ChecksumMismatch"},
		{"InvalidDigest", "InvalidChecksum"},
	} {
		t.Run(tc.native, func(t *testing.T) {
			upstream := minio.ErrorResponse{Code: tc.native, StatusCode: 400, Message: "private-object-and-credentials"}
			got := Public(upstream)
			if got.Status != 400 || got.Code != tc.public || strings.Contains(testmessage.Russian(got.Message), upstream.Message) {
				t.Fatalf("unsafe checksum classification: %+v", got)
			}
		})
	}
}

package transfers

import (
	"strings"

	validationsvc "github.com/owiks/golang-minio-s3-backend-api/src/services/validation"
)

func ValidTransferKey(key string) bool {
	return validationsvc.ValidFileKey(key) || strings.HasSuffix(key, "/") && validationsvc.ValidFileKey(strings.TrimSuffix(key, "/"))
}
func ValidTransferMapping(sourceKey, destinationKey, sourcePrefix, destinationPrefix string, sameBucket bool) bool {
	if !ValidTransferKey(sourceKey) || !ValidTransferKey(destinationKey) || strings.HasSuffix(sourceKey, "/") != strings.HasSuffix(destinationKey, "/") {
		return false
	}
	if sourcePrefix == "" {
		return destinationPrefix == ""
	}
	if !strings.HasSuffix(sourcePrefix, "/") || !strings.HasSuffix(destinationPrefix, "/") || !ValidTransferKey(sourcePrefix) || !ValidTransferKey(destinationPrefix) || !strings.HasPrefix(sourceKey, sourcePrefix) {
		return false
	}
	if destinationKey != destinationPrefix+strings.TrimPrefix(sourceKey, sourcePrefix) {
		return false
	}
	return !sameBucket || !strings.HasPrefix(destinationPrefix, sourcePrefix) && !strings.HasPrefix(sourcePrefix, destinationPrefix)
}

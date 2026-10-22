package runtime

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/upstream"
)

// Only the scan executor uses this adapter. It must not turn an uncertain
// transfer or other mutating request into an automatically repeatable command.
func operationReadError(err error) error {
	if upstream.IsThrottled(err) {
		return operations.ErrThrottled
	}
	return err
}

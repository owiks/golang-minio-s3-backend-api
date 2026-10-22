package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/minio/minio-go/v7"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/operations"
)

func TestOperationReadErrorDetachesOnlyThrottling(t *testing.T) {
	raw := minio.ErrorResponse{Code: "SlowDown", StatusCode: 503, Message: "private endpoint and credentials", Key: "private/key"}
	if got := operationReadError(raw); got != operations.ErrThrottled || got.Error() != "storage request throttled" {
		t.Fatal("throttling did not become a detached domain error")
	}
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, minio.ErrorResponse{Code: "AccessDenied", StatusCode: 403}, errors.New("invalid S3 XML"), nil} {
		if got := operationReadError(err); got != err {
			t.Fatal("non-throttling failure changed")
		}
	}
}

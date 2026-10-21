package buckets

import (
	"context"
	"testing"

	"minio-web/api/src/services/apperror"
)

// Invalid inputs must fail before any upstream call or pointer dereference.
func TestSettingValidationPrecedesUpstreamCalls(t *testing.T) {
	if _, err := QuotaChange(context.Background(), nil, "bucket", UpdateBucketQuotaRequest{}); apperror.Public(err).Code != "InvalidQuota" {
		t.Fatal(err)
	}
	if _, err := PolicyChange(context.Background(), nil, "bucket", UpdateBucketPolicyRequest{}); apperror.Public(err).Code != "InvalidPolicy" {
		t.Fatal(err)
	}
	if err := CreateBucket(context.Background(), nil, nil, CreateBucketRequest{Name: "INVALID"}); apperror.Public(err).Code != "InvalidBucketName" {
		t.Fatal(err)
	}
}

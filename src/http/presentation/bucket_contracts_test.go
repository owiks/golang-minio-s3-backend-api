package presentation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/buckets"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

func TestDetailsPreservePartialSelectionAndPolicyRevision(t *testing.T) {
	document := []byte(`{"Version":"2012-10-17","Statement":[],"FutureNumber":9007199254740993}`)
	decoder := json.NewDecoder(strings.NewReader(string(document)))
	decoder.UseNumber()
	var original any
	if err := decoder.Decode(&original); err != nil {
		t.Fatal(err)
	}
	typed := buckets.PolicyDocument(document)
	if buckets.SettingField(original, nil).Revision != buckets.SettingField(typed, nil).Revision {
		t.Fatal("policy revision changed after typing")
	}
	field := buckets.Detail(nil, apperror.New(403, "AccessDenied", messages.Literal("denied")))
	raw, err := json.Marshal(Render(context.Background(), buckets.BucketDetails{Name: "bucket", Policy: &field}))
	if err != nil || !sameJSON(raw, []byte(`{"name":"bucket","policy":{"error":"denied","errorCode":"AccessDenied"}}`)) {
		t.Fatalf("partial details changed: %s %v", raw, err)
	}
}

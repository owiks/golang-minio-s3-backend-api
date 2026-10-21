package presentation

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads"
)

func TestIncompleteUploadResultPreservesOmittedFieldsAndFalse(t *testing.T) {
	disabled := false
	cases := []struct {
		value any
		want  string
	}{
		{uploads.IncompleteUploadState{State: "absent"}, `{"state":"absent"}`},
		{uploads.AbortUploadResult{State: "absent"}, `{"state":"absent"}`},
		{uploads.AbortUploadResult{State: "remaining", OK: &disabled, Message: messages.Literal("pending")}, `{"state":"remaining","ok":false,"message":"pending"}`},
		{uploads.IncompleteUploadsPage{Items: []uploads.IncompleteUploadRow{}}, `{"items":[],"hasMore":false,"keyMarker":"","uploadIdMarker":""}`},
	}
	for _, tc := range cases {
		raw, err := json.Marshal(Render(context.Background(), tc.value))
		if err != nil || !sameJSON(raw, []byte(tc.want)) {
			t.Fatalf("wire contract changed: %s %v", raw, err)
		}
	}
}

func sameJSON(first, second []byte) bool {
	var a, b any
	return json.Unmarshal(first, &a) == nil && json.Unmarshal(second, &b) == nil && reflect.DeepEqual(a, b)
}

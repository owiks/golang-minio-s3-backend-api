package uploads

import (
	"encoding/json"
	"testing"
)

func TestIncompleteUploadResultPreservesOmittedFieldsAndFalse(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{IncompleteUploadState{State: "absent"}, `{"state":"absent"}`},
		{AbortUploadResult{State: "absent"}, `{"state":"absent"}`},
		{AbortUploadResult{State: "remaining", OK: boolPointer(false), Message: "pending"}, `{"state":"remaining","ok":false,"message":"pending"}`},
		{IncompleteUploadsPage{Items: []IncompleteUploadRow{}}, `{"items":[],"hasMore":false,"keyMarker":"","uploadIdMarker":""}`},
	}
	for _, tc := range cases {
		raw, err := json.Marshal(tc.value)
		if err != nil || string(raw) != tc.want {
			t.Fatalf("wire contract changed: %s %v", raw, err)
		}
	}
}

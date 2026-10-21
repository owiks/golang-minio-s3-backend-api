package result

import (
	"encoding/json"
	"testing"

	"minio-web/api/src/services/apperror"
)

func TestStreamEventsPreserveWireEnvelope(t *testing.T) {
	type done struct {
		OK bool `json:"ok"`
	}
	for _, tc := range []struct {
		value StreamEvent[done]
		want  string
	}{
		{StreamEvent[done]{Progress: &Progress{}}, `{"progress":{"bytes":0,"total":0}}`},
		{StreamEvent[done]{Data: &ByteChunk{Encoding: "base64", Bytes: "0Y8="}}, `{"data":{"encoding":"base64","bytes":"0Y8="}}`},
		{StreamEvent[done]{Result: &done{OK: false}}, `{"result":{"ok":false}}`},
		{StreamEvent[done]{Error: &apperror.Error{Status: 403, Code: "AccessDenied", Message: "denied"}}, `{"error":{"code":"AccessDenied","message":"denied"}}`},
	} {
		raw, err := json.Marshal(tc.value)
		if err != nil || string(raw) != tc.want {
			t.Fatalf("stream shape changed: %s %v", raw, err)
		}
	}
}

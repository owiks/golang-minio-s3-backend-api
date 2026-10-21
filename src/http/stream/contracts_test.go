package stream

import (
	"context"
	"encoding/json"
	"testing"

	"minio-web/api/src/http/presentation"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/result"
)

func TestStreamEventsPreserveWireEnvelope(t *testing.T) {
	type done struct {
		OK bool `json:"ok"`
	}
	for _, tc := range []struct {
		value Event[done]
		want  string
	}{
		{Event[done]{Progress: &result.Progress{}}, `{"progress":{"bytes":0,"total":0}}`},
		{Event[done]{Data: &result.ByteChunk{Encoding: "base64", Bytes: "0Y8="}}, `{"data":{"encoding":"base64","bytes":"0Y8="}}`},
		{Event[done]{Result: &done{OK: false}}, `{"result":{"ok":false}}`},
		{Event[done]{Error: presentation.Problem(context.Background(), apperror.Public(apperror.New(403, "AccessDenied", messages.Literal("denied"))))}, `{"error":{"code":"AccessDenied","message":"denied"}}`},
	} {
		raw, err := json.Marshal(tc.value)
		if err != nil || string(raw) != tc.want {
			t.Fatalf("stream shape changed: %s %v", raw, err)
		}
	}
}

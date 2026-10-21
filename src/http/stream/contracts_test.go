package stream

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/presentation"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/result"
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

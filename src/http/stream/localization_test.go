package stream

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/requestctx"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/result"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
)

func TestStreamLocalizesTerminalMessageWithoutChangingEvents(t *testing.T) {
	for _, lang := range []string{"en", "ru"} {
		for _, failure := range []bool{true, false} {
			ctx := requestctx.WithLanguage(context.Background(), lang, true, localization.Default().For(lang))
			w := httptest.NewRecorder()
			raw := []byte("Данные пользователя\x00\xff")
			WriteNDJSON(ctx, w, func(_ context.Context, send func(result.StreamEvent[transfers.TransferResult]) bool) error {
				send(result.StreamEvent[transfers.TransferResult]{Progress: &result.Progress{Bytes: 1, Total: 2}})
				send(result.StreamEvent[transfers.TransferResult]{Data: &result.ByteChunk{Encoding: "base64", Bytes: base64.StdEncoding.EncodeToString(raw)}})
				if failure {
					return apperror.New(409, "Changed", messages.UploadInterrupted)
				}
				final := transfers.TransferResult{Message: messages.UploadInterrupted}
				send(result.StreamEvent[transfers.TransferResult]{Result: &final})
				return nil
			})
			dec := json.NewDecoder(bytes.NewReader(w.Body.Bytes()))
			var event map[string]json.RawMessage
			if err := dec.Decode(&event); err != nil || event["progress"] == nil {
				t.Fatal("missing progress", w.Body, err)
			}
			var chunk Event[any]
			if err := dec.Decode(&chunk); err != nil || chunk.Data == nil {
				t.Fatal("missing data", err)
			}
			got, err := base64.StdEncoding.DecodeString(chunk.Data.Bytes)
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatal("user bytes changed")
			}
			var terminal struct {
				Error  *struct{ Code, Message string }
				Result *struct{ Message string }
			}
			if err := dec.Decode(&terminal); err != nil {
				t.Fatal(err)
			}
			want := localization.Default().For(lang).Render(messages.UploadInterrupted)
			if failure {
				if terminal.Error == nil || terminal.Error.Code != "Changed" || terminal.Error.Message != want || terminal.Result != nil {
					t.Fatal(w.Body)
				}
			} else if terminal.Result == nil || terminal.Result.Message != want || terminal.Error != nil {
				t.Fatal(w.Body)
			}
			if err := dec.Decode(&event); err != io.EOF || w.Code != 200 || w.Header().Get("Content-Language") != lang {
				t.Fatal("extra terminal or wrong headers", err, w.Body, w.Header())
			}
		}
	}
}

func TestUnknownStreamMessageCannotExposeIDOrArguments(t *testing.T) {
	for _, lang := range []string{"en", "ru"} {
		ctx := requestctx.WithLanguage(context.Background(), lang, true, localization.Default().For(lang))
		w := httptest.NewRecorder()
		WriteNDJSON(ctx, w, func(_ context.Context, send func(result.StreamEvent[transfers.TransferResult]) bool) error {
			send(result.StreamEvent[transfers.TransferResult]{Progress: &result.Progress{Bytes: 1, Total: 2}})
			return apperror.New(409, "SourceChanged", messages.ID("internal.missing.stream_message").With(messages.String("Token", "PRIVATE-TEMPLATE-DATA")))
		})
		decoder := json.NewDecoder(w.Body)
		var progress Event[any]
		if err := decoder.Decode(&progress); err != nil || progress.Progress == nil {
			t.Fatal("progress lost", err)
		}
		var terminal Event[any]
		if err := decoder.Decode(&terminal); err != nil || terminal.Error == nil || terminal.Error.Code != "SourceChanged" || terminal.Error.Message == "" || strings.Contains(terminal.Error.Message, "internal.") || strings.Contains(terminal.Error.Message, "PRIVATE-TEMPLATE-DATA") {
			t.Fatal("unsafe terminal", terminal, err)
		}
		if err := decoder.Decode(&terminal); err != io.EOF || w.Code != 200 {
			t.Fatal("terminal contract changed", err, w.Code)
		}
	}
}

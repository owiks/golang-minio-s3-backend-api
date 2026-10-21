package common

import (
	"encoding/json"
	"net/http"

	"minio-web/api/src/http/presentation"
	"minio-web/api/src/http/requestctx"
	"minio-web/api/src/services/messages"
)

func WriteJSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	data, err := json.Marshal(presentation.Render(r.Context(), value))
	if err != nil {
		status = http.StatusInternalServerError
		if observed, ok := w.(interface{ Failure(string) }); ok {
			observed.Failure("ResponseEncodingFailed")
		}
		data, _ = json.Marshal(map[string]any{"error": map[string]string{"code": "InternalError", "message": requestctx.Localizer(r.Context()).Render(messages.InternalError)}})
	}
	presentation.LanguageHeaders(r.Context(), w.Header())
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n'))
}

type Success struct {
	OK bool `json:"ok"`
}

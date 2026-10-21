package common_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/middleware"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
)

func TestUnprojectedMessageFailsSafelyBeforeHeaders(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Language", "ru")
	r = middleware.LocalizeRequest(r, "auto", nil)
	w := httptest.NewRecorder()
	common.WriteJSON(w, r, 201, struct {
		Unprojected messages.Ref `json:"newField"`
	}{messages.UploadSizeMismatch})
	if w.Code != 500 || !strings.Contains(w.Body.String(), `"code":"InternalError"`) || strings.Contains(w.Body.String(), "newField") || strings.Contains(w.Body.String(), string(messages.UploadSizeMismatch)) || w.Header().Get("Content-Language") != "ru" {
		t.Fatal(w.Code, w.Body, w.Header())
	}
}

func TestUnknownMessageKeepsResultAndNeverExposesIDOrParameters(t *testing.T) {
	for _, lang := range []string{"en", "ru"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Language", lang)
		r = middleware.LocalizeRequest(r, "auto", nil)
		w := httptest.NewRecorder()
		message := messages.ID("internal.unregistered.message").With(messages.String("Credential", "PRIVATE-TEMPLATE-DATA"))
		common.WriteJSON(w, r, 200, transfers.TransferResult{Copied: true, Moved: false, Code: "DeleteUncertain", Message: message})
		var value struct {
			Copied, Moved bool
			Code, Message string
		}
		if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || !value.Copied || value.Moved || value.Code != "DeleteUncertain" || value.Message == "" || strings.Contains(w.Body.String(), "internal.unregistered") || strings.Contains(w.Body.String(), "PRIVATE-TEMPLATE-DATA") {
			t.Fatal(w.Code, w.Body)
		}
	}
}

package validation

import (
	"bytes"
	"encoding/json"

	"minio-web/api/src/services/apperror"
)

func ValidateDocument(raw json.RawMessage) error {
	if len(raw) > 20*1024 {
		return apperror.New(400, "InvalidPolicy", "Политика не должна превышать 20 КиБ.")
	}
	var doc struct {
		Version   string          `json:"Version"`
		Statement json.RawMessage `json:"Statement"`
	}
	if json.Unmarshal(raw, &doc) != nil || doc.Version != "2012-10-17" || len(bytes.TrimSpace(doc.Statement)) == 0 || bytes.Equal(bytes.TrimSpace(doc.Statement), []byte("null")) {
		return apperror.New(400, "InvalidPolicy", "Укажите JSON политики с Version 2012-10-17 и Statement.")
	}
	return nil
}

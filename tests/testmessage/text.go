// Package testmessage renders messages for assertions outside HTTP tests.
package testmessage

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

func Russian(ref messages.Ref) string { return localization.Default().For("ru").Render(ref) }
func English(ref messages.Ref) string { return localization.Default().For("en").Render(ref) }
func Public(code string, ref messages.Ref) map[string]string {
	return map[string]string{"code": code, "message": English(ref)}
}

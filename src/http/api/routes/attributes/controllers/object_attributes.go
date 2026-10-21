package controllers

import (
	"net/http"
	"strconv"

	httpcommon "minio-web/api/src/http/api/common"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
	"minio-web/api/src/services/objects"
	"minio-web/api/src/services/runtime"
)

func HandleGetObjectAttributes(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session) error {
	q := r.URL.Query()
	maxParts, marker := 0, 0
	for _, field := range []struct {
		name  string
		value *int
	}{{"maxParts", &maxParts}, {"partNumberMarker", &marker}} {
		if q.Has(field.name) {
			value, err := strconv.Atoi(q.Get(field.name))
			if err != nil {
				return apperror.New(400, "InvalidObjectAttributes", messages.HttpInvalidObjectAttributes)
			}
			*field.value = value
			if field.name == "maxParts" && value == 0 {
				return apperror.New(400, "InvalidObjectAttributes", messages.HttpInvalidObjectAttributesObjectAttributes)
			}
		}
	}
	value, err := objects.GetAttributes(r.Context(), runtime.NewClients(s), objects.ObjectAttributesInput{Bucket: q.Get("bucket"), Key: q.Get("key"), VersionID: q.Get("versionId"), MaxParts: maxParts, PartNumberMarker: marker, Snapshot: q.Get("snapshot")})
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, http.StatusOK, value)
	return nil
}

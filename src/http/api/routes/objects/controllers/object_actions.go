package controllers

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	httpcommon "minio-web/api/src/http/api/common"
	"minio-web/api/src/http/stream"
	httpvalidation "minio-web/api/src/http/validation"
	"minio-web/api/src/services/apperror"
	filessvc "minio-web/api/src/services/files"
	"minio-web/api/src/services/messages"
	objectssvc "minio-web/api/src/services/objects"
	"minio-web/api/src/services/result"
	runtimesvc "minio-web/api/src/services/runtime"
)

func HandleListBuckets(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	value, err := filessvc.ListBuckets(r.Context(), runtimesvc.NewClients(s))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleListObjects(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	q := r.URL.Query()
	value, err := filessvc.ListObjects(r.Context(), runtimesvc.NewClients(s), filessvc.ObjectListOptions{Bucket: q.Get("bucket"), Prefix: q.Get("prefix"), Token: q.Get("token")})
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleGetObjectInfo(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	q := r.URL.Query()
	value, err := filessvc.GetObjectInfo(r.Context(), runtimesvc.NewClients(s), q.Get("bucket"), q.Get("key"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleGetObjectPreview(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	q := r.URL.Query()
	value, err := filessvc.GetPreview(r.Context(), runtimesvc.NewClients(s), q.Get("bucket"), q.Get("key"))
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", value.ContentType)
	w.Header().Set("Content-Disposition", "inline")
	_, err = w.Write(value.Data)
	return err
}
func HandleGetObjectLink(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	q := r.URL.Query()
	expires, _ := strconv.Atoi(q.Get("expiresIn"))
	value, err := filessvc.GetObjectLink(r.Context(), runtimesvc.NewClients(s), q.Get("bucket"), q.Get("key"), filessvc.ObjectLinkOptions{Mode: q.Get("mode"), ExpiresIn: expires})
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleGetObjectContent(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	q := r.URL.Query()
	value, err := objectssvc.GetContent(r.Context(), runtimesvc.NewClients(s), q.Get("bucket"), q.Get("key"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleUpdateObjectContent(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var body objectssvc.ObjectContentRequest
	if err := httpvalidation.DecodeLimit(w, r, &body, 6*objectssvc.MaxTextContent+4096); err != nil {
		return err
	}
	q := r.URL.Query()
	value, err := objectssvc.UpdateContent(r.Context(), runtimesvc.NewClients(s), q.Get("bucket"), q.Get("key"), body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleGetObjectProperties(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	q := r.URL.Query()
	value, err := objectssvc.GetProperties(r.Context(), runtimesvc.NewClients(s), q.Get("bucket"), q.Get("key"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleUpdateObjectProperties(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var body objectssvc.ObjectPropertiesRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	q := r.URL.Query()
	value, err := objectssvc.UpdateProperties(r.Context(), runtimesvc.NewClients(s), q.Get("bucket"), q.Get("key"), body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func validateSingleDelete(r *http.Request) error {
	if r.URL.Query().Has("version") || r.URL.Query().Has("versionId") {
		return apperror.New(400, "InvalidDelete", messages.HttpInvalidDelete)
	}
	return nil
}
func HandleInspectObjectDeletion(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if err := validateSingleDelete(r); err != nil {
		return err
	}
	q := r.URL.Query()
	value, err := objectssvc.InspectDeletion(r.Context(), runtimesvc.NewClients(s), q.Get("bucket"), q.Get("key"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleDeleteObject(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var body objectssvc.ObjectDeleteRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	if err := validateSingleDelete(r); err != nil {
		return err
	}
	q := r.URL.Query()
	value, err := objectssvc.DeleteObject(r.Context(), runtimesvc.NewClients(s), q.Get("bucket"), q.Get("key"), body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

type ObjectChecksumRequest struct {
	SHA256 string `json:"sha256"`
}

func HandleGetChecksum(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	bucket, key, err := objectssvc.ObjectParams(r.URL.Query().Get("bucket"), r.URL.Query().Get("key"))
	if err != nil {
		return err
	}
	run, err := objectssvc.PrepareChecksum(r.Context(), runtimesvc.NewClients(s), bucket, key, "", false)
	if err != nil {
		return err
	}
	defer run.Close()
	httpcommon.WriteJSON(w, r, 200, run.Summary())
	return nil
}
func HandleCalculateChecksum(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	bucket, key, err := objectssvc.ObjectParams(r.URL.Query().Get("bucket"), r.URL.Query().Get("key"))
	if err != nil {
		return err
	}
	var body ObjectChecksumRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	run, err := objectssvc.PrepareChecksum(r.Context(), runtimesvc.NewClients(s), bucket, key, strings.ToLower(strings.TrimSpace(body.SHA256)), true)
	if err != nil {
		return err
	}
	defer run.Close()
	stream.WriteNDJSON(r.Context(), w, func(ctx context.Context, send func(result.StreamEvent[objectssvc.ChecksumResult]) bool) error {
		return run.Stream(ctx, send)
	})
	return nil
}
func HandleListObjectVersions(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	q := r.URL.Query()
	value, err := filessvc.GetVersions(r.Context(), runtimesvc.NewClients(s), q.Get("bucket"), q.Get("key"), q.Get("cursor"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleGetVersionLink(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	q := r.URL.Query()
	value, err := filessvc.GetVersionLink(r.Context(), runtimesvc.NewClients(s), q.Get("bucket"), q.Get("key"), q.Get("version"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleGetVersionProtection(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	q := r.URL.Query()
	value, err := filessvc.GetVersionProtection(r.Context(), runtimesvc.NewClients(s), q.Get("bucket"), q.Get("key"), q.Get("version"))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}
func HandleUpdateVersionProtection(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, kind filessvc.ProtectionKind) error {
	var body filessvc.ObjectVersionsRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	q := r.URL.Query()
	value, err := filessvc.UpdateVersionProtection(r.Context(), runtimesvc.NewClients(s), q.Get("bucket"), q.Get("key"), q.Get("version"), kind, body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

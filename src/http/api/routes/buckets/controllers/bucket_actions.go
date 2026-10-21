package controllers

import (
	httpcommon "minio-web/api/src/http/api/common"
	httpvalidation "minio-web/api/src/http/validation"
	bucketssvc "minio-web/api/src/services/buckets"
	filessvc "minio-web/api/src/services/files"
	runtimesvc "minio-web/api/src/services/runtime"
	"net/http"
)

func HandleListBuckets(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	value, err := filessvc.ListBuckets(r.Context(), runtimesvc.NewClients(s))
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}

func HandleCreateBucket(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	var body bucketssvc.CreateBucketRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	if err := bucketssvc.CreateBucket(r.Context(), app.MutationLocker(s), runtimesvc.NewClients(s), body); err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, httpcommon.Success{OK: true})
	return nil
}
func HandleDeleteBucket(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, name string) error {
	if err := httpvalidation.Confirmation(w, r, name); err != nil {
		return err
	}
	if err := bucketssvc.DeleteBucket(r.Context(), runtimesvc.NewClients(s), name); err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, httpcommon.Success{OK: true})
	return nil
}
func HandleReadBucketLifecycle(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, bucket string) error {
	value, err := bucketssvc.ReadLifecycle(r.Context(), runtimesvc.NewClients(s), bucket)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, bucketssvc.LifecycleField(value, nil))
	return nil
}
func HandleChangeBucketLifecycle(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, bucket, operation string) error {
	var body bucketssvc.LifecycleChangeRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	value, err := bucketssvc.ChangeLifecycle(r.Context(), runtimesvc.NewClients(s), bucket, operation, body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}
func HandleReadBucketBrowserSetting(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, bucket, kind string) error {
	value, err := bucketssvc.ReadBrowserSetting(r.Context(), runtimesvc.NewClients(s), bucket, kind)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, bucketssvc.BrowserSettingField(value, nil))
	return nil
}
func HandleChangeBucketBrowserSetting(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, bucket, kind, operation string) error {
	var body bucketssvc.BrowserChangeRequest
	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	value, err := bucketssvc.ChangeBrowserSetting(r.Context(), runtimesvc.NewClients(s), bucket, kind, operation, body)
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}

package controllers

import (
	"net/http"

	httpcommon "minio-web/api/src/http/api/common"
	httpvalidation "minio-web/api/src/http/validation"
	"minio-web/api/src/services/apperror"
	bucketssvc "minio-web/api/src/services/buckets"
	runtimesvc "minio-web/api/src/services/runtime"
)

func HandleUpdateBucketSetting(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session, bucket, kind string) error {
	var value bucketssvc.DetailField
	var err error
	switch kind {
	case "policy":
		var body bucketssvc.UpdateBucketPolicyRequest
		if err := httpvalidation.Decode(w, r, &body); err != nil {
			return err
		}
		value, err = bucketssvc.PolicyChange(r.Context(), runtimesvc.NewClients(s), bucket, body)
	case "quota":
		var body bucketssvc.UpdateBucketQuotaRequest
		if err := httpvalidation.Decode(w, r, &body); err != nil {
			return err
		}
		value, err = bucketssvc.QuotaChange(r.Context(), runtimesvc.NewClients(s), bucket, body)
	case "tags":
		var body bucketssvc.UpdateBucketTagsRequest
		if err := httpvalidation.Decode(w, r, &body); err != nil {
			return err
		}
		value, err = bucketssvc.TagsChange(r.Context(), runtimesvc.NewClients(s), bucket, body)
	default:
		return apperror.New(405, "MethodNotAllowed", "Эта настройка не поддерживается.")
	}
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, 200, value)
	return nil
}

package controllers

import (
	"net/http"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	httpvalidation "github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	bucketssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/buckets"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
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
		return apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowedBucketUpdate)
	}
	if err != nil {
		return err
	}
	httpcommon.WriteJSON(w, r, 200, value)
	return nil
}

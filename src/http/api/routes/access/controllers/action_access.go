package controllers

import (
	"net/http"
	"strings"

	"github.com/minio/pkg/v3/policy"

	httpcommon "github.com/owiks/golang-minio-s3-backend-api/src/http/api/common"
	httpvalidation "github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	accesssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/access"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/iam"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	runtimesvc "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func HandleActionAccess(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "POST" {
		return apperror.New(405, "MethodNotAllowed", messages.HttpMethodNotAllowed)
	}
	var body ActionAccessRequest

	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	if len(body.Queries) == 0 || len(body.Queries) > 128 {
		return apperror.New(400, "InvalidPermissionQueries", messages.HttpInvalidPermissionQueries)
	}
	for _, q := range body.Queries {
		a := policy.Action(q.Action)
		if (!a.IsValid() && !policy.AdminAction(q.Action).IsValid()) || len(q.Bucket) > 256 || len(q.Key) > 4096 || len(q.Version) > 1024 {
			return apperror.New(400, "InvalidPermissionQueries", messages.HttpInvalidPermissionQueriesActionAccess)
		}
		if !strings.HasPrefix(q.Action, "s3:") && !strings.HasPrefix(q.Action, "admin:") {
			return apperror.New(400, "InvalidPermissionQueries", messages.HttpInvalidPermissionQueriesActionAccessHandleActionAccess)
		}
	}
	snapshot := s.Actions()
	result := make([]bool, len(body.Queries))
	for i, q := range body.Queries {
		result[i] = snapshot.Allows(q)
	}
	httpcommon.WriteJSON(w, r, 200, accesssvc.ActionAccess{Allowed: result, Known: snapshot.Known(), Issue: snapshot.Issue()})
	return nil
}

type ActionAccessRequest struct {
	Queries []iam.Query `json:"queries"`
}

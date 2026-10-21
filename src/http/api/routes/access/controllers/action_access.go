package controllers

import (
	"net/http"
	"strings"

	"github.com/minio/pkg/v3/policy"

	httpcommon "minio-web/api/src/http/api/common"
	httpvalidation "minio-web/api/src/http/validation"
	accesssvc "minio-web/api/src/services/access"
	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/iam"
	runtimesvc "minio-web/api/src/services/runtime"
)

func HandleActionAccess(app *runtimesvc.State, w http.ResponseWriter, r *http.Request, s *runtimesvc.Session) error {
	if r.Method != "POST" {
		return apperror.New(405, "MethodNotAllowed", "Метод не поддерживается.")
	}
	var body ActionAccessRequest

	if err := httpvalidation.Decode(w, r, &body); err != nil {
		return err
	}
	if len(body.Queries) == 0 || len(body.Queries) > 128 {
		return apperror.New(400, "InvalidPermissionQueries", "Укажите от 1 до 128 проверок.")
	}
	for _, q := range body.Queries {
		a := policy.Action(q.Action)
		if (!a.IsValid() && !policy.AdminAction(q.Action).IsValid()) || len(q.Bucket) > 256 || len(q.Key) > 4096 || len(q.Version) > 1024 {
			return apperror.New(400, "InvalidPermissionQueries", "Некорректная проверка действия.")
		}
		if !strings.HasPrefix(q.Action, "s3:") && !strings.HasPrefix(q.Action, "admin:") {
			return apperror.New(400, "InvalidPermissionQueries", "Неподдерживаемое действие.")
		}
	}
	snapshot := s.Actions()
	result := make([]bool, len(body.Queries))
	for i, q := range body.Queries {
		result[i] = snapshot.Allows(q)
	}
	httpcommon.WriteJSON(w, 200, accesssvc.ActionAccess{Allowed: result, Known: snapshot.Known(), Issue: snapshot.Issue()})
	return nil
}

type ActionAccessRequest struct {
	Queries []iam.Query `json:"queries"`
}

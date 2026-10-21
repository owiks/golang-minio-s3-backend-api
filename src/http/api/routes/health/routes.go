package health

import (
	"net/http"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/status"
	resultdto "github.com/owiks/golang-minio-s3-backend-api/src/services/result"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{{Path: "/health", Public: true, Tag: "status", Operations: []routes.Operation{
		{Method: "GET", Summary: "Проверка доступности API",
			Input:      routes.Inputs(),
			Controller: "Health",
			Response:   resultdto.Success{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
				status.Health(w, r)
				return nil
			}},
	},
	}}
}

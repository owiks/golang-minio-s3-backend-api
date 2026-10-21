package health

import (
	"net/http"

	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/http/status"
	resultdto "minio-web/api/src/services/result"
	"minio-web/api/src/services/runtime"
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

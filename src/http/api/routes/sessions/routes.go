package sessions

import (
	"net/http"

	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/http/api/routes/sessions/controllers"
	resultdto "minio-web/api/src/services/result"
	"minio-web/api/src/services/runtime"
	"minio-web/api/src/services/sessions"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{Path: "/api/sessions", Public: true, Tag: "sessions", Operations: []routes.Operation{
			{Method: "POST", Summary: "Подключение к S3/MinIO",
				Input:      routes.Inputs(routes.Header("Origin", true)),
				Controller: "HandleLogin",
				Request:    runtime.Connection{},
				Response:   sessions.LoginResponse{}, Status: 201, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
					return controllers.HandleLogin(app, w, r)
				}},
		},
		},
		{Path: "", Tag: "sessions", Operations: []routes.Operation{
			{Method: "DELETE", Summary: "Завершение сессии",
				Input:      routes.Inputs(),
				Controller: "HandleLogout",
				Response:   resultdto.Success{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
					return controllers.HandleLogout(app, w, r, s)
				}},
		},
		},
	}
}

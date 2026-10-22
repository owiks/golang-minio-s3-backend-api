package health

import (
	"net/http"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/healthresponse"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/status"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{{Path: "/health", Public: true, Tag: "status", Operations: []routes.Operation{
		{Method: "GET", Summary: "Проверка доступности API",
			Input:      routes.Inputs(),
			Controller: "Health",
			Response:   healthresponse.Response{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
				status.Health(w, r)
				return nil
			}},
	},
	}, {Path: "/ready", Public: true, Tag: "status", Operations: []routes.Operation{
		{Method: "GET", Summary: "Проверка готовности API",
			Input:      routes.Inputs().Describe("Проверяет доступность хранилища подключений и токенов с таймаутом 1 секунда. Не обращается к пользовательским S3-серверам. 503 StateUnavailable означает, что API пока не готов обслуживать запросы."),
			Controller: "Ready", Response: healthresponse.Response{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
				return status.Ready(w, r, app.Auth.Check)
			}},
	}}}
}

package sessions

import (
	"net/http"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/api/routes/sessions/controllers"
	resultdto "github.com/owiks/golang-minio-s3-backend-api/src/services/result"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/sessions"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{Path: "/api/tokens", Public: true, Tag: "sessions", Operations: []routes.Operation{
			{Method: "POST", Summary: "Выдача Bearer-токена",
				Input:      routes.Inputs(routes.Header("Origin", false)).Describe(tokenDescription),
				Controller: "HandleToken", Request: sessions.TokenRequest{}, Response: sessions.TokenResponse{}, Status: 201,
				Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
					return controllers.HandleToken(app, w, r)
				}},
		}},
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
			{Method: "DELETE", Summary: "Завершение сессии / отзыв Bearer-токена",
				Input:      routes.Inputs().Describe("Удаляет текущую сессию; следующие запросы с её токеном возвращают 401. Уже выполняющиеся запросы могут завершиться. Другие сессии и ключи MinIO сохраняются."),
				Controller: "HandleLogout",
				Response:   resultdto.Success{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
					return controllers.HandleLogout(app, w, r, s)
				}},
		},
		},
	}
}

const tokenDescription = `Принимает ключи S3/MinIO, создаёт отдельную сессию без cookie и возвращает токен один раз.
ttlSeconds — целое число от 1 до API_TOKEN_TTL в секундах; без поля используется API_TOKEN_TTL (по умолчанию 1 час).
Origin необязателен; если передан, должен совпадать с origin API/APP_ORIGIN. Для сетевого доступа используйте HTTPS.

Пример тела POST /api/tokens:

    {
      "endpoint": "https://s3.example.com",
      "accessKey": "YOUR_ACCESS_KEY",
      "secretKey": "YOUR_SECRET_KEY",
      "region": "us-east-1",
      "ttlSeconds": 900
    }

Ответ содержит id, token, tokenType, expiresAt и expiresIn. Используйте полученные id и token в запросах:

    curl -H 'Authorization: Bearer ACCESS_TOKEN' https://api.example.com/api/sessions/SESSION_ID/admin/buckets

Отзыв текущего токена:

    curl -X DELETE -H 'Authorization: Bearer ACCESS_TOKEN' https://api.example.com/api/sessions/SESSION_ID

Cookie, Origin и X-CSRF-Token для этих Bearer-запросов не нужны. После отзыва, истечения срока или перезапуска API получите новый токен.
`

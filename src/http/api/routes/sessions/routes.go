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
				Input:      routes.Inputs().Describe("Отзывает текущий доступ и зависимые Bearer-токены; следующие запросы возвращают 401. Выход из cookie-сессии сохраняет выданные через неё самостоятельные сервисные токены. Уже выполняющиеся запросы могут завершиться. Независимые доступы и ключи MinIO сохраняются."),
				Controller: "HandleLogout",
				Response:   resultdto.Success{}, Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
					return controllers.HandleLogout(app, w, r, s)
				}},
		},
		},
	}
}

const tokenDescription = `Принимает ключи S3/MinIO, создаёт отдельную сессию без cookie и возвращает токен один раз.
ttlSeconds — целое число от 0 до API_TOKEN_TTL в секундах; 0 создаёт бессрочный токен, без поля используется API_TOKEN_TTL (по умолчанию 1 час). Бессрочный ответ содержит expiresAt=0001-01-01T00:00:00Z и expiresIn=0. Cookie-сессии остаются конечными.
Origin необязателен; если передан, должен совпадать с origin API/APP_ORIGIN. Для сетевого доступа используйте HTTPS.

Пример тела POST /api/tokens:

    {
      "endpoint": "https://s3.example.com",
      "accessKey": "YOUR_ACCESS_KEY",
      "secretKey": "YOUR_SECRET_KEY",
      "region": "us-east-1",
      "ttlSeconds": 900
    }

Ответ содержит id, connectionID, revision, token, tokenType, expiresAt и expiresIn. Используйте полученные id и token в запросах:

    curl -H 'Authorization: Bearer ACCESS_TOKEN' https://api.example.com/api/sessions/SESSION_ID/admin/buckets

Отзыв текущего токена:

    curl -X DELETE -H 'Authorization: Bearer ACCESS_TOKEN' https://api.example.com/api/sessions/SESSION_ID

Cookie, Origin и X-CSRF-Token для этих Bearer-запросов не нужны. До истечения срока собственный токен с renewable=true можно продлить через POST /api/sessions/{id}/tokens/{id}/renew с revision и ttlSeconds; ID, секрет и права сохраняются. После отзыва или истечения срока получите новый токен. В postgres/mysql/sqlite доступ сохраняется после перезапуска; memory остаётся временным режимом.
`

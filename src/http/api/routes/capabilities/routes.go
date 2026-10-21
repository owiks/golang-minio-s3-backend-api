package capabilities

import (
	"net/http"

	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/http/api/routes/capabilities/controllers"
	capabilitiesdto "minio-web/api/src/services/capabilities"
	"minio-web/api/src/services/runtime"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{{Path: "capabilities", Tag: "access", Operations: []routes.Operation{{
		Method: "GET", Summary: "Возможности хранилища в текущей сессии",
		Input:      routes.Inputs(routes.Query("bucket", false, 63), routes.Query("key", false, 1024), routes.Query("versionId", false, 1024), routes.Query("feature", false, 80).Describe("Необязательный идентификатор возможности. Без feature возвращается весь каталог. Проверяются только ограниченные чтения; записи, Select и browser CORS остаются unknown до подтверждённой операции. Результаты действуют для текущей сессии, IAM revision и точной области bucket/key/versionId; checkedAt/expiresAt задают TTL. supported не заменяет авторизацию и нативные условия записи.")),
		Controller: "HandleCapabilities", Response: capabilitiesdto.Snapshot{},
		Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
			return controllers.HandleCapabilities(app, w, r, s)
		},
	}}}}
}

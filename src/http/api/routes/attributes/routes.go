package attributes

import (
	"net/http"

	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/http/api/routes/attributes/controllers"
	"minio-web/api/src/services/objects"
	"minio-web/api/src/services/runtime"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{
		{
			Path: "objects/attributes",
			Tag:  "objects",
			Operations: []routes.Operation{
				{
					Method:     http.MethodGet,
					Summary:    "Расширенные атрибуты и части объекта",
					Controller: "HandleGetObjectAttributes",
					Input: routes.Inputs(
						routes.Query("bucket", true, 63),
						routes.Query("key", true, 1024),
						routes.Query("versionId", false, 1024),
						routes.IntegerQuery("maxParts", false, 1, 1000).Describe("По умолчанию 100; читается одна страница."),
						routes.IntegerQuery("partNumberMarker", false, 0, 10000),
						routes.Query("snapshot", false, 64).Describe("Снимок предыдущей страницы обязателен при продолжении без неизменяемого versionId."),
					).Describe("GET требует cookie сессии; CSRF не требуется. Отсутствующие атрибуты не заменяются нулём; parts=null означает, что сервер не предоставил сведения о частях. consistency=version относится только к ненулевой версии; для observed snapshot обнаруживает наблюдаемую замену, но не гарантирует атомарный снимок изменяемого объекта. Следующую страницу запрашивайте с фактическим versionId, если он не null, либо с snapshot и nextPartNumberMarker. NoSuchVersion/NoSuchKey:404; AccessDenied:403; неподдерживаемый протокол:501; некорректный ответ:502; изменение между страницами:409. Запрос безопасно повторяемый, автоматического скачивания объекта нет."),
					Response: objects.ObjectAttributes{},
					Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
						return controllers.HandleGetObjectAttributes(app, w, r, s)
					},
				},
			},
		},
	}
}

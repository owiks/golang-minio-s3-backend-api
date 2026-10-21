package selectquery

import (
	"net/http"

	"minio-web/api/src/http/api/routes"
	"minio-web/api/src/http/api/routes/selectquery/controllers"
	"minio-web/api/src/services/result"
	"minio-web/api/src/services/runtime"
	"minio-web/api/src/services/selectquery"
)

func GetRoutes() []routes.APIRoute {
	return []routes.APIRoute{{Path: "objects/select", Tag: "objects", Operations: []routes.Operation{{
		Method: "POST", Summary: "S3 Select внутри CSV/JSON", Controller: "HandleSelectObject", Long: true, Mutation: routes.NoLock,
		Input:   routes.Inputs(routes.Query("bucket", true, 63), routes.Query("key", true, 1024)).Describe("Сессионный API с Origin/CSRF. CSV или JSON, без сжатия; SQL до 16 КиБ, тело до 32 КиБ. maxResultBytes 1–33554432 (по умолчанию 8388608), timeoutSeconds 1–30 (по умолчанию 20). HEAD допускает исходный объект до 256 МиБ; ограничение относится к проверенному снимку, не является атомарным лимитом сканирования. versionId и Range не поддерживаются. Unknown capability не блокирует запрос, unsupported/denied возвращаются безопасной ошибкой. Данные идут NDJSON data:{encoding:base64,bytes:...}, до 32 КиБ декодированных байтов в событии. Чанк не является полной записью: конкатенируйте декодированные байты перед UTF-8/CSV/JSON. Единственное терминальное result.complete=true появляется после валидного End и повторной проверки снимка; error означает неполный результат, даже при HTTP 200. Итоговая statistics может отсутствовать. До начала потока ошибки идут обычным JSON. Отмена/timeout закрывает upstream и прерывает заблокированную доставку данных; на доставку терминальной ошибки после завершения выполнения отводится не больше 1 секунды. Повтор запроса выполняет чтение заново. Upstream: кадр 1 МиБ, headers 8 КиБ, служебное событие 64 КиБ, поток 64 МиБ/8192 события; превышение останавливает запрос. Параллелизм ограничен общим admission передач API. Обработка уже принятых сервером данных отменяется best-effort."),
		Request: selectquery.Request{}, Required: []string{"expression", "input", "output"},
		Responses: []routes.Response{{Status: 200, MediaType: "application/x-ndjson", Model: result.StreamEvent[selectquery.Result]{}}},
		Handler: func(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session, rest string) error {
			return controllers.HandleSelectObject(app, w, r, s)
		},
	}}}}
}
